package testkit

import (
	"context"
	"math"
	"reflect"
	"time"

	"gochen/contextx"
	"gochen/domain"
	"gochen/domain/audited"
	"gochen/domain/crud"
	"gochen/domain/meta"
	"gochen/domain/spec"
	"gochen/errors"
)

// CheckTransferEntry 验证迁移用例在内存事务外开始。
func (r *MemoryRepository[T, ID]) CheckTransferEntry(ctx context.Context) error {
	if err := validateContext(ctx); err != nil {
		return err
	}
	_, lifecycle := contextx.TxLifecycleFromContext(ctx)
	_, active := r.transactionFromContext(ctx)
	if lifecycle || active {
		return errors.NewCode(errors.FailedPrecondition, "transfer must begin outside a transaction")
	}
	return nil
}

// Transfer 在内存事务中原子迁移显式允许的维度并推进版本。
func (r *MemoryRepository[T, ID]) Transfer(ctx context.Context, id ID, expectedVersion uint64, changes []crud.DimensionChange, using, check spec.Spec) (crud.TransferResult[T], error) {
	var result crud.TransferResult[T]
	if err := validateContext(ctx); err != nil {
		return result, err
	}
	if _, ok := r.transactionFromContext(ctx); !ok {
		return result, errors.NewCode(errors.FailedPrecondition, "transfer requires an active transaction")
	}
	if err := r.guardCrossIsolationWrite(ctx); err != nil {
		return result, err
	}
	if expectedVersion == math.MaxUint64 || len(changes) == 0 {
		return result, errors.NewCode(errors.InvalidInput, "invalid transfer version or empty changes")
	}
	if err := r.execution.ValidateWriteSpec(using, false); err != nil {
		return result, err
	}
	if err := r.execution.ValidateWriteSpec(check, false); err != nil {
		return result, err
	}
	isolationID, isolated, err := r.isolationID(ctx)
	if err != nil {
		return result, err
	}
	err = r.withWriteState(ctx, func(state *memoryState[T, ID]) error {
		before, exists := state.entities[id]
		if !exists || softDeleted(before) || (isolated && !r.visibleInIsolation(before, isolationID)) {
			return entityNotFound(id)
		}
		visible, err := r.matchSpec(using, before)
		if err != nil {
			return err
		}
		if !visible {
			return entityNotFound(id)
		}
		if before.GetVersion() != expectedVersion {
			return concurrencyError(id, expectedVersion)
		}
		after, err := meta.Clone(before)
		if err != nil {
			return err
		}
		seen := map[meta.Dimension]bool{}
		var actualChanges []crud.DimensionChange
		for _, change := range changes {
			if seen[change.Dimension] || !r.execution.CanTransfer(change.Dimension) {
				return errors.NewCode(errors.InvalidInput, "transfer dimension is duplicated or lacks capability")
			}
			seen[change.Dimension] = true
			field, err := r.metadata.Resolve(meta.ByDimension(change.Dimension))
			if err != nil {
				return err
			}
			converted, err := field.Convert(change.Value)
			if err != nil {
				return err
			}
			target, ok := field.Value(after)
			if !ok || !target.CanSet() {
				return errors.NewCode(errors.InvalidInput, "transfer field is not settable")
			}
			if reflect.DeepEqual(target.Interface(), converted.Interface()) {
				continue
			}
			target.Set(converted)
			value, err := field.Read(after)
			if err != nil {
				return err
			}
			actualChanges = append(actualChanges, crud.DimensionChange{Dimension: change.Dimension, Value: value})
		}
		if len(actualChanges) == 0 {
			return errors.NewCode(errors.InvalidInput, "transfer has no actual dimension change")
		}
		if actor, ok := any(after).(audited.IAuditable); ok {
			actor.SetUpdatedInfo(contextx.Operator(ctx), time.Now())
		}
		setter, ok := any(after).(domain.ISettableVersion)
		if !ok {
			return errors.NewCode(errors.InvalidInput, "transfer requires version support")
		}
		setter.SetVersion(expectedVersion + 1)
		if validatable, ok := any(after).(domain.IValidatable); ok {
			if err := validatable.Validate(); err != nil {
				return err
			}
		}
		if err := r.checkWriteSpec(check, after); err != nil {
			return err
		}
		state.entities[id] = cloneEntityState(after)
		result = crud.TransferResult[T]{Before: cloneEntityState(before), After: after, Changes: actualChanges}
		return nil
	})
	return result, err
}
