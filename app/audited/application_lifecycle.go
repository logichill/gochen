package audited

import (
	"context"
	"strings"
	"time"

	appcrud "gochen/app/crud"
	"gochen/app/internal/writeflow"
	"gochen/contextx"
	"gochen/domain"
	domaudited "gochen/domain/audited"
	"gochen/domain/crud"
	"gochen/errors"
)

// Delete 执行软删除（透传到 ISoftDeletable 实现）并写入审计 AuditOpDelete。
//
// 约束：
// - ctx 中必须包含 operator，否则返回 InvalidInput；
// - 物理删除请使用 Purge。
func (s *Application[T, ID]) Delete(ctx context.Context, id ID) error {
	if err := requireAuditOperator(ctx); err != nil {
		return err
	}
	hooks := s.Application.Hooks()
	return writeflow.New[ID](s.txRepo).
		Action(s.softDeletePersist).
		Before(hooks.Delete.Before).
		After(hooks.Delete.After).
		After(s.afterIDAudit(id, domaudited.AuditOpDelete)).
		PostCommit(hooks.Delete.PostCommit).
		Run(ctx, id)
}

// Restore 恢复已软删的实体，并记录审计。
//
// 参数：
// - by：本次恢复操作的 operator（会写入审计记录）。
func (s *Application[T, ID]) Restore(ctx context.Context, id ID, by string) error {
	by = strings.TrimSpace(by)
	if by == "" {
		return errors.NewCode(errors.InvalidInput, "audit operator is required")
	}
	// 入口校验后立即向 ctx 注入 operator：Update.Before 钩子、仓储 Update
	// 及审计记录都依赖 ctx 中的 operation 身份，缺失会丢失审计归属。
	// 注入必须发生在 writeflow 之前：事务内装载/写回阶段拿到的 ctx 由
	// writeflow 传递，若在流程内部才注入，下游钩子与仓储仍会丢失审计归属。
	ctx, err := contextx.WithOperator(ctx, by)
	if err != nil {
		return err
	}
	r := &restoreFlow[T, ID]{app: s, id: id, by: by, auditCtx: ctx}
	return writeflow.New[ID](s.txRepo).
		Action(r.write).
		After(r.afterHook).
		After(r.saveRecord).
		PostCommit(r.postCommitHook).
		Run(ctx, id)
}

// Purge 执行物理删除（永久删除），并记录审计。
//
// 约束：
//   - ctx 中必须包含 operator；
//   - repo 必须实现 crud.IPurgeRepository，否则返回 Unsupported（能力缺失是
//     仓储契约问题而非调用方输入错误，与 crud 包对可选能力的错误语义保持一致）。
func (s *Application[T, ID]) Purge(ctx context.Context, id ID) error {
	r, ok := any(s.Repository()).(crud.IPurgeRepository[T, ID])
	if !ok {
		return errors.NewCode(errors.Unsupported, "repository does not support purge")
	}
	if err := requireAuditOperator(ctx); err != nil {
		return err
	}
	hooks := s.Application.Hooks()
	// Purge 使用独立的 Purge 钩子；未配置时回退 Delete 钩子（兼容既有行为）。
	// 物理清除不应重放软删除的业务副作用，也不应被 Delete.Before 的
	// 活跃态校验卡死（已软删记录对常规读不可见）。
	purgeStage := hooks.Purge
	if purgeStage.IsZero() {
		purgeStage = hooks.Delete
	}
	return writeflow.New[ID](s.txRepo).
		Action(r.Purge).
		Before(purgeStage.Before).
		After(purgeStage.After).
		After(s.afterIDAudit(id, domaudited.AuditOpDeleteHard)).
		PostCommit(purgeStage.PostCommit).
		Run(ctx, id)
}

// ListDeleted 分页读取已软删除的实体。
// 与 crud List 同口径规范化分页入参，防止负 offset 下推 SQL、巨型 limit 全表加载。
func (s *Application[T, ID]) ListDeleted(ctx context.Context, offset, limit int) ([]T, error) {
	offset, limit = appcrud.NormalizePageBounds(offset, limit, s.MaxPageSize())
	return s.deletedRepo.ListDeleted(ctx, offset, limit)
}

// GetWithDeleted 按主键读取实体，并允许命中软删除记录。
func (s *Application[T, ID]) GetWithDeleted(ctx context.Context, id ID) (T, error) {
	return s.restoreRepo.GetWithDeleted(ctx, id)
}

// HasPurgeCapability 报告底层仓储是否支持硬删除。
func (s *Application[T, ID]) HasPurgeCapability() bool {
	_, ok := s.Repository().(crud.IPurgeRepository[T, ID])
	return ok
}

func (s *Application[T, ID]) softDeletePersist(ctx context.Context, id ID) error {
	entity, err := s.softDeleteEntity(ctx, id)
	if err != nil {
		return err
	}
	return s.Repository().Update(ctx, entity)
}

func (s *Application[T, ID]) softDeleteEntity(ctx context.Context, id ID) (T, error) {
	var zero T

	by := contextx.Operator(ctx)
	if by == "" {
		return zero, errors.NewCode(errors.InvalidInput, "audit operator is required")
	}

	entity, err := s.Repository().Get(ctx, id)
	if err != nil {
		return zero, err
	}
	ae, err := s.asAuditedEntity(entity)
	if err != nil {
		return zero, err
	}
	if err := ae.SoftDeleteBy(by, time.Now()); err != nil {
		return zero, err
	}
	return entity, nil
}

func (s *Application[T, ID]) afterIDAudit(id ID, op domaudited.AuditOperation) func(context.Context, ID) error {
	return func(ctx context.Context, _ ID) error {
		return s.saveAudit(ctx, id, op, nil)
	}
}

// restoreFlow 承载恢复流程的跨阶段状态：事务内装载的恢复实体与审计上下文。
// restoreFlow 管理实体恢复的写流水线。
type restoreFlow[T domain.IEntity[ID], ID comparable] struct {
	app      *Application[T, ID]
	id       ID
	by       string
	entity   T
	auditCtx context.Context
}

func (r *restoreFlow[T, ID]) write(ctx context.Context, id ID) error {
	entity, err := r.app.restoreRepo.GetWithDeleted(ctx, id)
	if err != nil {
		return err
	}
	ae, err := r.app.asAuditedEntity(entity)
	if err != nil {
		return err
	}
	if !ae.IsDeleted() {
		return errors.NewCode(errors.Conflict, "entity not deleted").WithContext("id", id)
	}
	if err := ae.Restore(); err != nil {
		return err
	}
	// Restore 使用独立的 Restore 钩子；未配置时回退 Update 钩子（兼容既有行为）。
	if beforeHook := r.restoreStage().Before; beforeHook != nil {
		if err := beforeHook(ctx, entity); err != nil {
			return err
		}
	}
	if err := r.app.Application.Validate(entity); err != nil {
		return err
	}
	if err := r.app.Repository().Update(ctx, entity); err != nil {
		return err
	}
	// ctx 已经在 Restore 入口完成 operator 注入，这里直接复用，
	// 保证审计记录与下游钩子看到同一个操作人身份。
	r.entity = entity
	r.auditCtx = ctx
	return nil
}

// restoreStage 返回 Restore 动作应使用的钩子阶段：显式配置的 Restore 钩子
// 优先；未配置时回退 Update 钩子（兼容既有行为）。
// 恢复语义与常规更新不同，业务可为恢复配置专用副作用（如不发更新事件）。
func (r *restoreFlow[T, ID]) restoreStage() appcrud.Stage[T] {
	hooks := r.app.Application.Hooks()
	if !hooks.Restore.IsZero() {
		return hooks.Restore
	}
	return hooks.Update
}

func (r *restoreFlow[T, ID]) afterHook(ctx context.Context, _ ID) error {
	hook := r.restoreStage().After
	if hook == nil {
		return nil
	}
	return hook(ctx, r.entity)
}

func (r *restoreFlow[T, ID]) saveRecord(context.Context, ID) error {
	return r.app.saveAudit(r.auditCtx, r.id, domaudited.AuditOpRestore, nil)
}

// postCommitHook 以恢复后的实体触发 Restore（回退 Update）.PostCommit。
// 实体在事务内装载，无法作为流程载荷直接传递。
func (r *restoreFlow[T, ID]) postCommitHook(ctx context.Context, _ ID) error {
	hook := r.restoreStage().PostCommit
	if hook == nil {
		return nil
	}
	return hook(ctx, r.entity)
}
