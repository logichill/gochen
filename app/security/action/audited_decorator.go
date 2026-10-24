package action

import (
	"context"

	appaudited "gochen/app/audited"
	authaction "gochen/auth/action"
	"gochen/domain"
	"gochen/domain/audited"
)

// auditedSurface 是带动作校验的审计扩展实现。
//
// Purge / Restore 是高危写操作，AuditTrail / ListDeleted 是能看到已删数据的读操作，
// 三类都有独立动作码，不复用基础 CRUD 的 delete / read。
type auditedSurface[T domain.IEntity[ID], ID comparable] struct {
	app   *application[T, ID]
	inner appaudited.IAuditOperations[T, ID]
}

func (s *auditedSurface[T, ID]) Purge(ctx context.Context, id ID) error {
	if err := s.app.require(ctx, authaction.OpPurge); err != nil {
		return err
	}
	return s.inner.Purge(ctx, id)
}

func (s *auditedSurface[T, ID]) Restore(ctx context.Context, id ID, by string) error {
	if err := s.app.require(ctx, authaction.OpRestore); err != nil {
		return err
	}
	return s.inner.Restore(ctx, id, by)
}

func (s *auditedSurface[T, ID]) ListDeleted(ctx context.Context, offset, limit int) ([]T, error) {
	if err := s.app.require(ctx, authaction.OpAuditRead); err != nil {
		return nil, err
	}
	return s.inner.ListDeleted(ctx, offset, limit)
}

// GetWithDeleted 按主键读取实体（含已删记录）：与 ListDeleted 同级受控读。
// 若不纳入安全外壳，方法会从装饰后的能力面上消失；持有具体类型的调用方
// 将以裸调用绕过鉴权直接读取已删数据。
func (s *auditedSurface[T, ID]) GetWithDeleted(ctx context.Context, id ID) (T, error) {
	if err := s.app.require(ctx, authaction.OpAuditRead); err != nil {
		var zero T
		return zero, err
	}
	return s.inner.GetWithDeleted(ctx, id)
}

func (s *auditedSurface[T, ID]) AuditTrail(ctx context.Context, id ID, offset, limit int) ([]audited.AuditRecord, error) {
	if err := s.app.require(ctx, authaction.OpAuditRead); err != nil {
		return nil, err
	}
	return s.inner.AuditTrail(ctx, id, offset, limit)
}

// HasAuditStore 不是请求路径，仅报告底层审计存储是否就绪，不泄露存储句柄。
func (s *auditedSurface[T, ID]) HasAuditStore() bool { return s.inner.HasAuditStore() }

// HasPurgeCapability 保留内层硬删除能力声明；实际调用仍执行动作授权。
func (s *auditedSurface[T, ID]) HasPurgeCapability() bool { return s.inner.HasPurgeCapability() }
