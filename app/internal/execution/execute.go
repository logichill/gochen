package execution

import (
	"context"

	"gochen/app/internal/depcheck"
	"gochen/auth"
	"gochen/contextx"
	domaudited "gochen/domain/audited"
	"gochen/errors"
)

// Execute 修改已有实体：授权 → 事务{ Get → 快照 → fn → Before → 校验 → 无变更跳过 → Update → After → 审计 } → PostCommit。
func (c *Core[T, ID]) Execute(
	ctx context.Context,
	action string,
	id ID,
	fn func(ctx context.Context, e T) error,
	opts ...ExecOption,
) (T, error) {
	var zero T
	if err := checkContext(ctx); err != nil {
		return zero, err
	}
	if fn == nil {
		return zero, errors.NewCode(errors.InvalidInput, "command function cannot be nil")
	}
	spec, _, err := c.authorizeKind(ctx, action, []ID{id}, auth.KindUpdate)
	if err != nil {
		return zero, err
	}
	if err := ValidateWriteSpec(spec, false); err != nil {
		return zero, err
	}
	if depcheck.IsNil(c.tx) {
		return zero, errors.NewCode(errors.Internal, "repository does not support transactions")
	}
	act, err := c.auditActor(ctx, opts)
	if err != nil {
		return zero, err
	}
	var result T
	err = c.tx.WithinTx(ctx, func(txCtx context.Context) error {
		before, err := c.repo.Get(txCtx, id, spec)
		if err != nil {
			return err
		}
		entity, record, hasAudit, err := c.updateLoaded(ctx, txCtx, action, act, spec, before, before, fn)
		if err != nil {
			return err
		}
		result = entity
		if !hasAudit {
			return nil
		}
		return c.saveAudit(txCtx, record)
	})
	if err != nil && !contextx.IsAfterCommitError(err) {
		return zero, err
	}
	return result, err
}

// ExecuteNew 新建实体：授权 → 事务{ fn 产出实体 → Before → 赋值 → 校验 → Create → After → 审计 } → PostCommit。
func (c *Core[T, ID]) ExecuteNew(
	ctx context.Context,
	action string,
	fn func(ctx context.Context) (T, error),
	opts ...ExecOption,
) (T, error) {
	var zero T
	if err := checkContext(ctx); err != nil {
		return zero, err
	}
	if fn == nil {
		return zero, errors.NewCode(errors.InvalidInput, "command function cannot be nil")
	}
	spec, decision, err := c.authorizeKind(ctx, action, nil, auth.KindCreate)
	if err != nil {
		return zero, err
	}
	if err := ValidateWriteSpec(spec, true); err != nil {
		return zero, err
	}
	if depcheck.IsNil(c.tx) {
		return zero, errors.NewCode(errors.Internal, "repository does not support transactions")
	}
	act, err := c.auditActor(ctx, opts)
	if err != nil {
		return zero, err
	}
	var result T
	err = c.tx.WithinTx(ctx, func(txCtx context.Context) error {
		entity, err := fn(txCtx)
		if err != nil {
			return err
		}
		if depcheck.IsNil(entity) {
			return errors.NewCode(errors.InvalidInput, "command produced a nil entity")
		}
		record, hasAudit, err := c.createOne(ctx, txCtx, action, act, spec, decision, entity)
		if err != nil {
			return err
		}
		result = entity
		if !hasAudit {
			return nil
		}
		return c.saveAudit(txCtx, record)
	})
	if err != nil && !contextx.IsAfterCommitError(err) {
		return zero, err
	}
	return result, err
}

// ExecuteBulk 一次授权、一个事务修改多个实体；返回按输入顺序排列的实体。
func (c *Core[T, ID]) ExecuteBulk(
	ctx context.Context,
	action string,
	ids []ID,
	fn func(ctx context.Context, e T) error,
	opts ...ExecOption,
) ([]T, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if fn == nil {
		return nil, errors.NewCode(errors.InvalidInput, "command function cannot be nil")
	}
	if err := c.validateBulkIDs(ids); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return []T{}, nil
	}
	spec, _, err := c.authorizeKind(ctx, action, ids, auth.KindUpdate)
	if err != nil {
		return nil, err
	}
	if err := ValidateWriteSpec(spec, false); err != nil {
		return nil, err
	}
	if depcheck.IsNil(c.tx) {
		return nil, errors.NewCode(errors.Internal, "repository does not support transactions")
	}
	act, err := c.auditActor(ctx, opts)
	if err != nil {
		return nil, err
	}
	results := make([]T, 0, len(ids))
	err = c.tx.WithinTx(ctx, func(txCtx context.Context) error {
		records := make([]domaudited.AuditRecord, 0, len(ids))
		for _, id := range ids {
			before, err := c.repo.Get(txCtx, id, spec)
			if err != nil {
				return err
			}
			entity, record, hasAudit, err := c.updateLoaded(ctx, txCtx, action, act, spec, before, before, fn)
			if err != nil {
				return err
			}
			results = append(results, entity)
			if hasAudit {
				records = append(records, record)
			}
		}
		return c.saveAudits(txCtx, records)
	})
	if err != nil && !contextx.IsAfterCommitError(err) {
		return nil, err
	}
	return results, err
}

// ExecuteNewBulk 一次授权（IDs 为空）、一个事务新建多个实体。
func (c *Core[T, ID]) ExecuteNewBulk(
	ctx context.Context,
	action string,
	fn func(ctx context.Context) ([]T, error),
	opts ...ExecOption,
) ([]T, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if fn == nil {
		return nil, errors.NewCode(errors.InvalidInput, "command function cannot be nil")
	}
	spec, decision, err := c.authorizeKind(ctx, action, nil, auth.KindCreate)
	if err != nil {
		return nil, err
	}
	if err := ValidateWriteSpec(spec, true); err != nil {
		return nil, err
	}
	if depcheck.IsNil(c.tx) {
		return nil, errors.NewCode(errors.Internal, "repository does not support transactions")
	}
	act, err := c.auditActor(ctx, opts)
	if err != nil {
		return nil, err
	}
	var results []T
	err = c.tx.WithinTx(ctx, func(txCtx context.Context) error {
		entities, err := fn(txCtx)
		if err != nil {
			return err
		}
		if len(entities) > c.limits.MaxBulkSize {
			return errors.NewCode(errors.InvalidInput, "bulk size exceeds the configured limit").
				WithContext("limit", c.limits.MaxBulkSize).
				WithContext("size", len(entities))
		}
		records := make([]domaudited.AuditRecord, 0, len(entities))
		results = make([]T, 0, len(entities))
		for _, entity := range entities {
			if depcheck.IsNil(entity) {
				return errors.NewCode(errors.InvalidInput, "command produced a nil entity")
			}
			record, hasAudit, err := c.createOne(ctx, txCtx, action, act, spec, decision, entity)
			if err != nil {
				return err
			}
			results = append(results, entity)
			if hasAudit {
				records = append(records, record)
			}
		}
		return c.saveAudits(txCtx, records)
	})
	if err != nil && !contextx.IsAfterCommitError(err) {
		return nil, err
	}
	return results, err
}

// Restore 恢复已软删实体：授权 → 事务{ GetWithDeleted → 领域恢复 → Before → 校验 → Restore → After → 审计 }。
func (c *Core[T, ID]) Restore(ctx context.Context, id ID, opts ...ExecOption) (T, error) {
	var zero T
	if err := checkContext(ctx); err != nil {
		return zero, err
	}
	if depcheck.IsNil(c.restore) || depcheck.IsNil(c.tx) {
		return zero, errors.NewCode(errors.Unsupported, "restore is not configured")
	}
	spec, _, err := c.authorizeKind(ctx, ActionRestore, []ID{id}, auth.KindUpdate)
	if err != nil {
		return zero, err
	}
	if err := ValidateWriteSpec(spec, false); err != nil {
		return zero, err
	}
	act, err := c.auditActor(ctx, opts)
	if err != nil {
		return zero, err
	}
	var result T
	err = c.tx.WithinTx(ctx, func(txCtx context.Context) error {
		entity, err := c.restore.GetWithDeleted(txCtx, id, spec)
		if err != nil {
			return err
		}
		audited, ok := any(entity).(domaudited.IAuditedEntity[ID])
		if !ok {
			return errors.NewCode(errors.Internal, "entity does not implement audited.IAuditedEntity")
		}
		if !audited.IsDeleted() {
			return errors.NewCode(errors.Conflict, "entity not deleted").
				WithContext("id", FormatID(id))
		}
		if err := audited.Restore(); err != nil {
			return err
		}
		stage := c.hooks.For(ActionRestore)
		if err := c.invokeBefore(txCtx, stage, entity); err != nil {
			return err
		}
		if err := c.validate(entity); err != nil {
			return err
		}
		if err := c.restore.Restore(txCtx, entity, spec); err != nil {
			return err
		}
		if err := c.invokeAfter(txCtx, stage, entity); err != nil {
			return err
		}
		if err := c.schedulePostCommit(txCtx, ctx, stage, entity); err != nil {
			return err
		}
		result = entity
		if depcheck.IsNil(c.audit) {
			return nil
		}
		return c.saveAudit(txCtx, c.record(txCtx, act, ActionRestore, entity, nil))
	})
	if err != nil && !contextx.IsAfterCommitError(err) {
		return zero, err
	}
	return result, err
}

// Purge 永久删除实体（含已软删记录）：授权 → 事务{ GetWithDeleted → Purge → 审计 }。
func (c *Core[T, ID]) Purge(ctx context.Context, id ID, opts ...ExecOption) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if depcheck.IsNil(c.purge) || depcheck.IsNil(c.restore) || depcheck.IsNil(c.tx) {
		return errors.NewCode(errors.Unsupported, "purge is not configured")
	}
	spec, _, err := c.authorizeKind(ctx, ActionPurge, []ID{id}, auth.KindDelete)
	if err != nil {
		return err
	}
	if err := ValidateWriteSpec(spec, false); err != nil {
		return err
	}
	act, err := c.auditActor(ctx, opts)
	if err != nil {
		return err
	}
	return c.tx.WithinTx(ctx, func(txCtx context.Context) error {
		entity, err := c.restore.GetWithDeleted(txCtx, id, spec)
		if err != nil {
			return err
		}
		stage := c.hooks.For(ActionPurge)
		if err := c.invokeBefore(txCtx, stage, entity); err != nil {
			return err
		}
		if err := c.purge.Purge(txCtx, entity, spec); err != nil {
			return err
		}
		if err := c.invokeAfter(txCtx, stage, entity); err != nil {
			return err
		}
		if err := c.schedulePostCommit(txCtx, ctx, stage, entity); err != nil {
			return err
		}
		if depcheck.IsNil(c.audit) {
			return nil
		}
		return c.saveAudit(txCtx, c.record(txCtx, act, ActionPurge, entity, nil))
	})
}
