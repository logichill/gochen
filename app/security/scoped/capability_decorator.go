package scoped

import (
	"context"

	"gochen/auth/action"
	authscoped "gochen/auth/scoped"

	"gochen/app/crud"
	"gochen/app/internal/writeflow"
	"gochen/app/security/internal/capability"
	"gochen/domain"
	"gochen/domain/audited"
	"gochen/errors"
)

// L3 可选能力面的资源授权。
//
// 批量与审计高危操作是与 Create/Update/Delete 平级的写入口，
// 不能原样转发给内层——那等于给未授权调用留一条绕过 PDP 的旁路。

// batchWriter 是带资源授权与约束投放的批量写实现。
//
// 约束按 §2.3.2 逐条构造：一次批量里每个目标资源各自经 PDP 判定，
// 汇总为一个覆盖全部目标的 IConstraintProvider 后一次性投放，
// 由仓储在同一事务内逐条下推原子约束。
type batchWriter[T domain.IEntity[ID], ID comparable] struct {
	app   *application[T, ID]
	inner interface {
		CreateAll(ctx context.Context, entities []T) error
		UpdateAll(ctx context.Context, entities []T) error
		DeleteAll(ctx context.Context, ids []ID) error
	}
}

func (b *batchWriter[T, ID]) CreateAll(ctx context.Context, entities []T) error {
	if err := b.requireBatchSize(len(entities)); err != nil {
		return err
	}
	scopedCtx, err := b.authorizeAll(ctx, action.OpCreate, toTargets(entities))
	if err != nil {
		return err
	}
	return b.inner.CreateAll(scopedCtx, entities)
}

func (b *batchWriter[T, ID]) UpdateAll(ctx context.Context, entities []T) error {
	if err := b.requireBatchSize(len(entities)); err != nil {
		return err
	}
	// 与单条 Update 同理：授权输入取自仓储的真实边界，不取客户端实体。
	ids := make([]ID, 0, len(entities))
	for i := range entities {
		ids = append(ids, entities[i].GetID())
	}
	targets, err := b.boundariesOf(ctx, ids)
	if err != nil {
		return err
	}
	scopedCtx, err := b.authorizeAll(ctx, action.OpUpdate, targets)
	if err != nil {
		return err
	}
	return b.inner.UpdateAll(scopedCtx, entities)
}

func (b *batchWriter[T, ID]) DeleteAll(ctx context.Context, ids []ID) error {
	if err := b.requireBatchSize(len(ids)); err != nil {
		return err
	}
	targets, err := b.boundariesOf(ctx, ids)
	if err != nil {
		return err
	}
	scopedCtx, err := b.authorizeAll(ctx, action.OpDelete, targets)
	if err != nil {
		return err
	}
	return b.inner.DeleteAll(scopedCtx, ids)
}

// requireBatchSize 在**授权之前**卡住批量规模。
//
// 内层 Application 也校验同一个上限，但它在 L3 转发之后才执行，而 L3 的
// 单次批量成本是 N 次边界解析 + N 次 PDP 判定 + N 条授权审计。等内层再拒，
// 这些成本已经全部付掉了——请求体上限（默认 10MB）折算成 ID 数量是六位数，
// 足以把一次必然失败的请求放大成一次数据库风暴。上限取自内层配置，
// 保证两处判据一致，不引入第二个真值来源。
func (b *batchWriter[T, ID]) requireBatchSize(size int) error {
	maxSize := 0
	if cfg := b.app.inner.Config(); cfg != nil {
		maxSize = cfg.MaxBatchSize
	}
	if maxSize <= 0 {
		// 与内层 normalizeServiceConfig 的兜底保持一致。
		maxSize = crud.DefaultServiceConfig().MaxBatchSize
	}
	return writeflow.ValidateBatchSize(size, maxSize)
}

// boundariesOf 解析一批 ID 在仓储中的真实授权边界，顺序与入参一致。
//
// 优先走批量探针（一次 IN 查询）；仓储只提供单条探针或完全没有探针时，
// 逐条降级到 resolveBoundary——降级路径的语义与批量路径等价，只是往返更多。
func (b *batchWriter[T, ID]) boundariesOf(ctx context.Context, ids []ID) ([]any, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	if reader, ok := any(b.app.inner.Repository()).(authscoped.IBatchResourceBoundaryReader[ID]); ok {
		resources, err := reader.ResolveResourcesByID(ctx, ids)
		if err != nil {
			return nil, err
		}
		if len(resources) != len(ids) {
			// 边界数量对不上就无法保证"每个目标都被判定过"，只能整批拒绝。
			return nil, errors.NewCode(errors.Forbidden, "resource boundary count does not match the batch targets").
				WithContext("expected", len(ids)).
				WithContext("actual", len(resources))
		}
		targets := make([]any, 0, len(resources))
		for i := range resources {
			targets = append(targets, resources[i])
		}
		return targets, nil
	}
	targets := make([]any, 0, len(ids))
	for _, id := range ids {
		target, err := b.app.resolveBoundary(ctx, id)
		if err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	return targets, nil
}

// authorizeAll 逐条判定并合并约束。
//
// 任一条不通过即整批拒绝——批量不是"尽力而为"，部分放行会让调用方
// 以为整批都被授权了。
func (b *batchWriter[T, ID]) authorizeAll(ctx context.Context, op action.Operation, targets []any) (context.Context, error) {
	if len(targets) == 0 {
		return ctx, nil
	}
	code, ok := b.app.config.Policy.CodeFor(op)
	if !ok {
		return nil, errors.NewCode(errors.Forbidden, "operation is not configured in action policy").
			WithContext("operation", string(op))
	}
	merged := authscoped.WriteConstraint{}
	for _, target := range targets {
		decision, err := b.app.authorizer.Authorize(ctx, code, target)
		if err != nil {
			return nil, err
		}
		if !decision.IsAllowed() {
			return nil, errors.NewCode(errors.Forbidden, "authorization denied").
				WithContext("action", code).
				WithContext("reason", decision.ReasonCode)
		}
		// 约束只增不减（§4.1 第 3 条）：逐条累加，不覆盖已有项。
		granted := decision.WriteConstraint().Normalize()
		merged.Resources = append(merged.Resources, granted.Resources...)
	}
	if merged.IsEmpty() {
		return nil, errors.NewCode(errors.Forbidden, "batch write is not authorized for any target").
			WithContext("action", code)
	}
	// 与单条写路径共用同一套 ctx 构造：约束与数据范围必须一起绑定，
	// 否则声明了范围列的仓储会在写路径解析不到范围而整批拒绝。
	return b.app.writeContext(ctx, merged)
}

func toTargets[T any](entities []T) []any {
	targets := make([]any, 0, len(entities))
	for i := range entities {
		targets = append(targets, entities[i])
	}
	return targets
}

// auditedSurface 是带资源授权的审计扩展实现（§2.3.3）。
type auditedSurface[T domain.IEntity[ID], ID comparable] struct {
	app   *application[T, ID]
	inner capability.IAuditedSurface[T, ID]
}

// Purge 物理擦除：先解析资源边界再判定，并经通道投放约束。
func (s *auditedSurface[T, ID]) Purge(ctx context.Context, id ID) error {
	scopedCtx, err := s.authorizeTarget(ctx, action.OpPurge, id)
	if err != nil {
		return err
	}
	return s.inner.Purge(scopedCtx, id)
}

func (s *auditedSurface[T, ID]) Restore(ctx context.Context, id ID, by string) error {
	scopedCtx, err := s.authorizeTarget(ctx, action.OpRestore, id)
	if err != nil {
		return err
	}
	return s.inner.Restore(scopedCtx, id, by)
}

// ListDeleted 读已删数据：按主体数据范围过滤。
func (s *auditedSurface[T, ID]) ListDeleted(ctx context.Context, offset, limit int) ([]T, error) {
	scopedCtx, err := s.app.authorizeRead(ctx, action.OpAuditRead)
	if err != nil {
		return nil, err
	}
	return s.inner.ListDeleted(scopedCtx, offset, limit)
}

// AuditTrail 读审计轨迹：目标资源须在数据范围内。
//
// 用 include-deleted 解析：查一条已删记录的轨迹是审计的主要用途。
func (s *auditedSurface[T, ID]) AuditTrail(ctx context.Context, id ID, offset, limit int) ([]audited.AuditRecord, error) {
	target, err := s.app.resolveDeletedBoundary(ctx, id)
	if err != nil {
		return nil, err
	}
	scopedCtx, err := s.app.authorizeRead(ctx, action.OpAuditRead, target)
	if err != nil {
		return nil, err
	}
	return s.inner.AuditTrail(scopedCtx, id, offset, limit)
}

func (s *auditedSurface[T, ID]) AuditStore() audited.IAuditStore { return s.inner.AuditStore() }

// authorizeTarget 解析资源边界 → PDP 判定 → 投放约束。
//
// 一律走 include-deleted 解析：Restore 的目标必然已软删，Purge 的目标通常也已软删。
// 用常规探针会被 `deleted_at IS NULL` 过滤掉，导致高危操作在 L3 下恒返回 NotFound。
func (s *auditedSurface[T, ID]) authorizeTarget(ctx context.Context, op action.Operation, id ID) (context.Context, error) {
	target, err := s.app.resolveDeletedBoundary(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.app.authorizeWrite(ctx, op, target)
}
