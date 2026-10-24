package scoped

import (
	"context"

	appaudited "gochen/app/audited"
	"gochen/auth/action"
	"gochen/domain"
	"gochen/domain/audited"
)

// auditedSurface 是带资源授权的审计扩展实现（§2.3.3）。
type auditedSurface[T domain.IEntity[ID], ID comparable] struct {
	app   *application[T, ID]
	inner appaudited.IAuditOperations[T, ID]
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

// GetWithDeleted 按主键读取实体（含已删记录）：与 ListDeleted 同级受控读，
// 目标资源须在数据范围内。若不纳入安全外壳，方法会从装饰后的能力面上消失；
// 持有具体类型的调用方将以裸调用绕过鉴权直接读取已删数据。
func (s *auditedSurface[T, ID]) GetWithDeleted(ctx context.Context, id ID) (T, error) {
	target, err := s.app.resolveDeletedBoundary(ctx, id)
	if err != nil {
		var zero T
		return zero, err
	}
	scopedCtx, err := s.app.authorizeRead(ctx, action.OpAuditRead, target)
	if err != nil {
		var zero T
		return zero, err
	}
	return s.inner.GetWithDeleted(scopedCtx, id)
}

// AuditTrail 读审计轨迹：目标资源须在数据范围内。
//
// 用 include-deleted 解析：查一条已删记录的轨迹是审计的主要用途。
// 实体已被物理（Purge）删除而无法解析边界时，按 NotFound 处理（fail-closed），
// 不做动作级降级直读：审计存储链路（IAuditStore.ListAuditRecordsByEntity）
// 只按实体 ID 查询，既不消费 ctx 中的 DataScope，审计记录也没有范围列可供
// 事后过滤——若在此降级为仅动作级 OpAuditRead 授权，持 audit_read 权限的
// 主体即可按任意 ID 越范围调阅被 Purge 实体的完整字段级审计史。
// 边界解析失败即按无权限处理，与 GetWithDeleted 的语义保持一致。
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

// HasAuditStore 报告底层审计存储是否就绪，不泄露存储句柄。
func (s *auditedSurface[T, ID]) HasAuditStore() bool { return s.inner.HasAuditStore() }

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

// HasPurgeCapability 保留内层硬删除能力声明；实际调用仍执行资源授权。
func (s *auditedSurface[T, ID]) HasPurgeCapability() bool { return s.inner.HasPurgeCapability() }
