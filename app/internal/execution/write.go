package execution

import (
	"context"
	"encoding/json"

	"gochen/app/operation"
	"gochen/auth"
	domaudited "gochen/domain/audited"
	"gochen/domain/spec"
	"gochen/errors"
	"gochen/internal/depcheck"
)

// Create 创建单个实体：授权 → 事务{ Before → 赋值 → 校验 → Create → After → 审计 } → PostCommit。
func (c *Core[T, ID]) Create(ctx context.Context, entity T) error {
	ctx = operation.EnsureOperationID(ctx)
	if err := checkContext(ctx); err != nil {
		return err
	}
	if depcheck.IsNil(entity) {
		return errors.NewCode(errors.InvalidInput, "entity cannot be nil")
	}
	spec, decision, err := c.authorizeKind(ctx, ActionCreate, nil, auth.KindCreate)
	if err != nil {
		return err
	}
	if err := c.execution.ValidateWriteSpec(spec, true); err != nil {
		return err
	}
	if depcheck.IsNil(c.tx) {
		return errors.NewCode(errors.Internal, "repository does not support transactions")
	}
	act, err := c.auditActor(ctx, nil)
	if err != nil {
		return err
	}
	return c.tx.WithinTx(ctx, func(txCtx context.Context) error {
		record, hasAudit, err := c.createOne(ctx, txCtx, ActionCreate, act, spec, decision, entity)
		if err != nil {
			return err
		}
		if !hasAudit {
			return nil
		}
		return c.saveAudit(txCtx, record)
	})
}

// Update 更新单个实体：授权 → 事务{ Get → 快照 → Before → 校验 → Update → After → 审计 } → PostCommit。
// 无业务字段变更时跳过写入与审计。
func (c *Core[T, ID]) Update(ctx context.Context, entity T) error {
	ctx = operation.EnsureOperationID(ctx)
	if err := checkContext(ctx); err != nil {
		return err
	}
	if depcheck.IsNil(entity) {
		return errors.NewCode(errors.InvalidInput, "entity cannot be nil")
	}
	id := entity.GetID()
	spec, _, err := c.authorizeKind(ctx, ActionUpdate, []ID{id}, auth.KindUpdate)
	if err != nil {
		return err
	}
	if err := c.execution.ValidateWriteSpec(spec, false); err != nil {
		return err
	}
	if depcheck.IsNil(c.tx) {
		return errors.NewCode(errors.Internal, "repository does not support transactions")
	}
	act, err := c.auditActor(ctx, nil)
	if err != nil {
		return err
	}
	return c.tx.WithinTx(ctx, func(txCtx context.Context) error {
		before, err := c.repo.Get(txCtx, id, spec)
		if err != nil {
			return err
		}
		_, record, hasAudit, err := c.updateLoaded(ctx, txCtx, ActionUpdate, act, spec, before, entity, nil)
		if err != nil {
			return err
		}
		if !hasAudit {
			return nil
		}
		return c.saveAudit(txCtx, record)
	})
}

// Delete 删除单个实体。audited 构造下为软删，其余为物理删。
func (c *Core[T, ID]) Delete(ctx context.Context, id ID) error {
	ctx = operation.EnsureOperationID(ctx)
	if err := checkContext(ctx); err != nil {
		return err
	}
	spec, _, err := c.authorizeKind(ctx, ActionDelete, []ID{id}, auth.KindDelete)
	if err != nil {
		return err
	}
	if err := c.execution.ValidateWriteSpec(spec, false); err != nil {
		return err
	}
	if depcheck.IsNil(c.tx) {
		return errors.NewCode(errors.Internal, "repository does not support transactions")
	}
	act, err := c.auditActor(ctx, nil)
	if err != nil {
		return err
	}
	return c.tx.WithinTx(ctx, func(txCtx context.Context) error {
		record, hasAudit, err := c.deleteOne(ctx, txCtx, ActionDelete, act, spec, id)
		if err != nil {
			return err
		}
		if !hasAudit {
			return nil
		}
		return c.saveAudit(txCtx, record)
	})
}

// DeleteBulk 一次授权、一个事务删除多个实体；任一失败整批回滚。
func (c *Core[T, ID]) DeleteBulk(ctx context.Context, ids []ID) error {
	ctx = operation.EnsureOperationID(ctx)
	if err := checkContext(ctx); err != nil {
		return err
	}
	if err := c.validateBulkIDs(ids); err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	spec, _, err := c.authorizeKind(ctx, ActionDelete, ids, auth.KindDelete)
	if err != nil {
		return err
	}
	if err := c.execution.ValidateWriteSpec(spec, false); err != nil {
		return err
	}
	if depcheck.IsNil(c.tx) {
		return errors.NewCode(errors.Internal, "repository does not support transactions")
	}
	act, err := c.auditActor(ctx, nil)
	if err != nil {
		return err
	}
	return c.tx.WithinTx(ctx, func(txCtx context.Context) error {
		records := make([]domaudited.AuditRecord, 0, len(ids))
		for _, id := range ids {
			record, hasAudit, err := c.deleteOne(ctx, txCtx, ActionDelete, act, spec, id)
			if err != nil {
				return err
			}
			if hasAudit {
				records = append(records, record)
			}
		}
		return c.saveAudits(txCtx, records)
	})
}

// createOne 在已开启的事务中创建实体，返回待写入的审计记录。
func (c *Core[T, ID]) createOne(
	ctx context.Context,
	txCtx context.Context,
	action string,
	act Actor,
	spec spec.Spec,
	decision auth.Decision,
	entity T,
) (domaudited.AuditRecord, bool, error) {
	stage := c.hooks.For(action)
	if err := c.invokeBefore(txCtx, stage, entity); err != nil {
		return domaudited.AuditRecord{}, false, err
	}
	if err := c.applyAssignments(entity, decision.Assign); err != nil {
		return domaudited.AuditRecord{}, false, err
	}
	if err := c.validate(entity); err != nil {
		return domaudited.AuditRecord{}, false, err
	}
	if err := c.repo.Create(txCtx, entity, spec); err != nil {
		return domaudited.AuditRecord{}, false, err
	}
	var snapshot json.RawMessage
	if !depcheck.IsNil(c.audit) {
		var err error
		snapshot, err = c.snapshot(entity)
		if err != nil {
			return domaudited.AuditRecord{}, false, err
		}
	}
	if err := c.invokeAfter(txCtx, stage, entity); err != nil {
		return domaudited.AuditRecord{}, false, err
	}
	if err := c.schedulePostCommit(txCtx, ctx, stage, entity); err != nil {
		return domaudited.AuditRecord{}, false, err
	}
	if depcheck.IsNil(c.audit) {
		return domaudited.AuditRecord{}, false, nil
	}
	record, err := c.record(txCtx, act, action, entity, snapshot)
	return record, true, err
}

// updateLoaded 在已开启的事务中更新实体。before 是事务内读取的当前状态，
// entity 是调用方或命令函数修改后的目标状态；无业务字段变更时跳过写入。
func (c *Core[T, ID]) updateLoaded(
	ctx context.Context,
	txCtx context.Context,
	action string,
	act Actor,
	spec spec.Spec,
	before T,
	entity T,
	fn func(context.Context, T) error,
) (T, domaudited.AuditRecord, bool, error) {
	stage := c.hooks.For(action)
	protected, err := c.execution.CaptureProtected(before)
	if err != nil {
		return entity, domaudited.AuditRecord{}, false, err
	}
	beforeState, err := c.businessSnapshot(before)
	if err != nil {
		return entity, domaudited.AuditRecord{}, false, err
	}
	var beforeSnapshot json.RawMessage
	if !depcheck.IsNil(c.audit) {
		beforeSnapshot, err = c.snapshot(before)
		if err != nil {
			return entity, domaudited.AuditRecord{}, false, err
		}
	}
	if fn != nil {
		if err := fn(txCtx, entity); err != nil {
			return entity, domaudited.AuditRecord{}, false, err
		}
	}
	if err := c.execution.CheckProtected(protected, entity); err != nil {
		return entity, domaudited.AuditRecord{}, false, err
	}
	// 写前钩子校验命令产出的目标状态，单条与批量更新遵循相同顺序。
	if err := c.invokeBefore(txCtx, stage, entity); err != nil {
		return entity, domaudited.AuditRecord{}, false, err
	}
	if err := c.execution.CheckProtected(protected, entity); err != nil {
		return entity, domaudited.AuditRecord{}, false, err
	}
	if err := c.validate(entity); err != nil {
		return entity, domaudited.AuditRecord{}, false, err
	}
	afterState, err := c.businessSnapshot(entity)
	if err != nil {
		return entity, domaudited.AuditRecord{}, false, err
	}
	businessChanges, err := c.diffSnapshots(beforeState, afterState)
	if err != nil {
		return entity, domaudited.AuditRecord{}, false, err
	}
	if businessChanges == nil {
		// 无业务变更：不写库、不写审计，版本保持不变。
		return entity, domaudited.AuditRecord{}, false, nil
	}
	if err := c.repo.Update(txCtx, entity, spec); err != nil {
		return entity, domaudited.AuditRecord{}, false, err
	}
	var changes json.RawMessage
	if !depcheck.IsNil(c.audit) {
		afterSnapshot, err := c.snapshot(entity)
		if err != nil {
			return entity, domaudited.AuditRecord{}, false, err
		}
		changes, err = c.diffSnapshots(beforeSnapshot, afterSnapshot)
		if err != nil {
			return entity, domaudited.AuditRecord{}, false, err
		}
	}
	if err := c.invokeAfter(txCtx, stage, entity); err != nil {
		return entity, domaudited.AuditRecord{}, false, err
	}
	if err := c.schedulePostCommit(txCtx, ctx, stage, entity); err != nil {
		return entity, domaudited.AuditRecord{}, false, err
	}
	if depcheck.IsNil(c.audit) {
		return entity, domaudited.AuditRecord{}, false, nil
	}
	record, err := c.record(txCtx, act, action, entity, changes)
	return entity, record, true, err
}

// businessSnapshot 捕获全部业务字段，独立于审计投影与 JSON 输出字段选择。
// 序列化立即冻结指针字段的当前值，供命令修改前后比较。
func (c *Core[T, ID]) businessSnapshot(entity T) (json.RawMessage, error) {
	state := make(map[string]any)
	for _, field := range c.metadata.Fields() {
		if field.Identity || field.Managed {
			continue
		}
		value, ok := field.Value(entity)
		if !ok {
			return nil, errors.NewCode(errors.InvalidInput, "business field cannot be read").WithContext("field", field.Name)
		}
		state[field.Name] = value.Interface()
	}
	return marshalJSON(state, "marshal business state")
}

// deleteOne 在已开启的事务中删除实体，返回待写入的审计记录。
func (c *Core[T, ID]) deleteOne(
	ctx context.Context,
	txCtx context.Context,
	action string,
	act Actor,
	spec spec.Spec,
	id ID,
) (domaudited.AuditRecord, bool, error) {
	entity, err := c.repo.Get(txCtx, id, spec)
	if err != nil {
		return domaudited.AuditRecord{}, false, err
	}
	stage := c.hooks.For(action)
	if err := c.invokeBefore(txCtx, stage, entity); err != nil {
		return domaudited.AuditRecord{}, false, err
	}
	if c.softDelete {
		audited, ok := any(entity).(domaudited.IAuditedEntity[ID])
		if !ok {
			return domaudited.AuditRecord{}, false, errors.NewCode(errors.Internal, "entity does not implement audited.IAuditedEntity")
		}
		if err := audited.SoftDeleteBy(act.Operator, nowUTC()); err != nil {
			return domaudited.AuditRecord{}, false, err
		}
		if err := c.repo.Update(txCtx, entity, spec); err != nil {
			return domaudited.AuditRecord{}, false, err
		}
	} else if err := c.repo.Delete(txCtx, entity, spec); err != nil {
		return domaudited.AuditRecord{}, false, err
	}
	if err := c.invokeAfter(txCtx, stage, entity); err != nil {
		return domaudited.AuditRecord{}, false, err
	}
	if err := c.schedulePostCommit(txCtx, ctx, stage, entity); err != nil {
		return domaudited.AuditRecord{}, false, err
	}
	if depcheck.IsNil(c.audit) {
		return domaudited.AuditRecord{}, false, nil
	}
	record, err := c.record(txCtx, act, action, entity, nil)
	return record, true, err
}

func (c *Core[T, ID]) validateBulkIDs(ids []ID) error {
	if len(ids) > c.limits.MaxBulkSize {
		return errors.NewCode(errors.InvalidInput, "bulk size exceeds the configured limit").
			WithContext("limit", c.limits.MaxBulkSize).
			WithContext("size", len(ids))
	}
	seen := make(map[ID]struct{}, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			return errors.NewCode(errors.InvalidInput, "duplicate id in bulk").
				WithContext("id", FormatID(id))
		}
		seen[id] = struct{}{}
	}
	return nil
}
