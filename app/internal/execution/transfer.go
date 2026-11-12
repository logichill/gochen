package execution

import (
	"context"
	"encoding/json"
	"math"
	"reflect"
	"strings"

	"gochen/app/operation"
	"gochen/auth"
	"gochen/contextx"
	domaudited "gochen/domain/audited"
	domcrud "gochen/domain/crud"
	"gochen/domain/meta"
	"gochen/errors"
	"gochen/internal/depcheck"
)

// Transfer 在两阶段授权后原子迁移维度并写入两侧审计记录。
func (c *Core[T, ID]) Transfer(ctx context.Context, action string, id ID, expectedVersion uint64, targets []auth.Assignment, opts ...ExecOption) (T, error) {
	var zero T
	if err := checkContext(ctx); err != nil {
		return zero, err
	}
	if c.commandGrant != nil {
		return zero, errors.NewCode(errors.FailedPrecondition, "command-bound applications do not support two-phase transfer")
	}
	if _, active := contextx.TxLifecycleFromContext(ctx); active {
		return zero, errors.NewCode(errors.FailedPrecondition, "transfer must begin outside a transaction")
	}
	declared, exists := c.actions[action]
	if !exists || declared.Kind != auth.KindTransfer || declared.ReceiveAction == "" {
		return zero, errors.NewCode(errors.Forbidden, "transfer action is not declared")
	}
	if len(targets) == 0 || expectedVersion == math.MaxUint64 {
		return zero, errors.NewCode(errors.InvalidInput, "transfer targets cannot be empty")
	}
	repository := c.repo.(domcrud.ITransferRepository[T, ID])
	if err := repository.CheckTransferEntry(ctx); err != nil {
		return zero, err
	}
	frozen := make([]auth.Assignment, 0, len(targets))
	seen := map[meta.Dimension]bool{}
	allowed := map[meta.Dimension]bool{}
	for _, dimension := range declared.Dimensions {
		allowed[dimension] = true
	}
	for _, target := range targets {
		if seen[target.Dimension] || !allowed[target.Dimension] {
			return zero, errors.NewCode(errors.InvalidInput, "duplicate transfer target")
		}
		seen[target.Dimension] = true
		field, err := c.metadata.Resolve(meta.ByDimension(target.Dimension))
		if err != nil {
			return zero, err
		}
		value, err := field.Convert(target.Value)
		if err != nil {
			return zero, err
		}
		var scalar any
		if value.Kind() == reflect.Ptr {
			if !value.IsNil() {
				scalar = value.Elem().Interface()
			}
		} else {
			scalar = value.Interface()
		}
		frozen = append(frozen, auth.Assignment{Dimension: target.Dimension, Value: scalar})
	}
	ctx = operation.EnsureOperationID(ctx)
	release, err := operation.ReserveTransfer(ctx, c.resource, FormatID(id))
	if err != nil {
		return zero, err
	}
	committed := false
	defer func() {
		if !committed {
			release()
		}
	}()
	using, outgoing, err := AuthorizeAction(ctx, c.resource, c.authorizer, c.descriptor, c.actions, c.vocabulary, c.metadata, action, []string{FormatID(id)}, frozen...)
	if err != nil {
		return zero, err
	}
	if len(outgoing.Assign) != 0 {
		return zero, errors.NewCode(errors.Internal, "transfer source decision must not assign targets")
	}
	check, incoming, err := AuthorizeAction(ctx, c.resource, c.authorizer, c.descriptor, c.actions, c.vocabulary, c.metadata, declared.ReceiveAction, []string{FormatID(id)}, frozen...)
	if err != nil {
		return zero, err
	}
	if using.IsNone() || check.IsNone() {
		return zero, errors.NewCode(errors.Forbidden, "transfer requires source and receive conditions")
	}
	if err := c.execution.ValidateWriteSpec(using, false); err != nil {
		return zero, err
	}
	if err := c.execution.ValidateWriteSpec(check, false); err != nil {
		return zero, err
	}
	if len(incoming.Assign) == 0 {
		return zero, errors.NewCode(errors.Internal, "transfer receive decision must assign targets")
	}
	changes := make([]domcrud.DimensionChange, 0, len(incoming.Assign))
	for _, assignment := range incoming.Assign {
		if !allowed[assignment.Dimension] {
			return zero, errors.NewCode(errors.Internal, "transfer assignment exceeds action dimensions")
		}
		field, err := c.metadata.Resolve(meta.ByDimension(assignment.Dimension))
		if err != nil {
			return zero, errors.Wrap(err, errors.Internal, "transfer assignment has no binding")
		}
		converted, err := field.Convert(assignment.Value)
		if err != nil {
			return zero, errors.Wrap(err, errors.Internal, "invalid authoritative transfer value")
		}
		var value any
		if converted.Kind() == reflect.Ptr {
			if !converted.IsNil() {
				value = converted.Elem().Interface()
			}
		} else {
			value = converted.Interface()
		}
		changes = append(changes, domcrud.DimensionChange{Dimension: assignment.Dimension, Value: value})
	}
	actor, err := c.auditActor(ctx, opts)
	if err != nil {
		return zero, err
	}
	var result T
	err = c.tx.WithinTx(ctx, func(txCtx context.Context) error {
		transfer, err := repository.Transfer(txCtx, id, expectedVersion, changes, using, check)
		if err != nil {
			return err
		}
		if depcheck.IsNil(transfer.Before) || depcheck.IsNil(transfer.After) || transfer.Before.GetID() != id || transfer.After.GetID() != id || transfer.Before.GetVersion() != expectedVersion || transfer.After.GetVersion() != expectedVersion+1 || len(transfer.Changes) == 0 {
			return errors.NewCode(errors.Internal, "repository returned an incomplete transfer result")
		}
		for _, changed := range transfer.Changes {
			if !allowed[changed.Dimension] {
				return errors.NewCode(errors.Internal, "repository returned an unauthorized transfer dimension")
			}
		}
		if err := c.validate(transfer.After); err != nil {
			return err
		}
		result = transfer.After
		if c.audit != nil {
			projected, err := c.transferChanges(transfer)
			if err != nil {
				return err
			}
			out, err := c.record(txCtx, actor, "transfer_out", transfer.Before, projected)
			if err != nil {
				return err
			}
			in, err := c.record(txCtx, actor, "transfer_in", transfer.After, projected)
			if err != nil {
				return err
			}
			if err := c.saveAudits(txCtx, []domaudited.AuditRecord{out, in}); err != nil {
				return err
			}
		}
		stage := c.hooks.For(action)
		if err := c.invokeAfter(txCtx, stage, transfer.After); err != nil {
			return err
		}
		return c.schedulePostCommit(txCtx, ctx, stage, transfer.After)
	})
	if err != nil && !contextx.IsAfterCommitError(err) {
		return zero, err
	}
	committed = true
	return result, err
}

func (c *Core[T, ID]) transferChanges(result domcrud.TransferResult[T]) (json.RawMessage, error) {
	before, err := c.snapshot(result.Before)
	if err != nil {
		return nil, err
	}
	after, err := c.snapshot(result.After)
	if err != nil {
		return nil, err
	}
	oldValues, err := unmarshalSnapshot(before)
	if err != nil {
		return nil, err
	}
	newValues, err := unmarshalSnapshot(after)
	if err != nil {
		return nil, err
	}
	var changes []domaudited.FieldChange
	for _, changed := range result.Changes {
		field, _ := c.metadata.DimensionField(changed.Dimension)
		jsonName := field.Name
		if structField, ok := c.metadata.Type().FieldByName(field.Name); ok {
			if name := strings.Split(structField.Tag.Get("json"), ",")[0]; name != "" {
				jsonName = name
			}
		}
		old, oldExists := oldValues[jsonName]
		new, newExists := newValues[jsonName]
		if (!oldExists && !newExists) || reflect.DeepEqual(old, new) {
			continue
		}
		changes = append(changes, domaudited.FieldChange{Field: jsonName, Old: old, New: new})
	}
	return marshalJSON(changes, "marshal transfer changes")
}
