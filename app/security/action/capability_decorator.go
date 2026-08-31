package action

import (
	"context"

	authaction "gochen/auth/action"

	"gochen/app/security/internal/capability"
	"gochen/domain"
	"gochen/domain/audited"
)

// L1 可选能力面的动作级加固。
//
// 为什么这些能力不能"原样转发给内层"：批量写与 Purge / Restore 都是写入口，
// 直接转发等于给未授权调用留了一条绕过 Create/Update/Delete 校验的旁路。
// 因此每个能力面都在此重新校验一次动作码，再交给内层。

// batchWriter 是带动作校验的批量写实现。
type batchWriter[T domain.IEntity[ID], ID comparable] struct {
	app   *application[T, ID]
	inner interface {
		CreateAll(ctx context.Context, entities []T) error
		UpdateAll(ctx context.Context, entities []T) error
		DeleteAll(ctx context.Context, ids []ID) error
	}
}

// CreateAll 校验 create 动作后批量创建。
//
// 批量与单条共用同一动作码：一次操作的动作语义不因条数而改变。
func (b *batchWriter[T, ID]) CreateAll(ctx context.Context, entities []T) error {
	if err := b.app.require(ctx, authaction.OpCreate); err != nil {
		return err
	}
	return b.inner.CreateAll(ctx, entities)
}

func (b *batchWriter[T, ID]) UpdateAll(ctx context.Context, entities []T) error {
	if err := b.app.require(ctx, authaction.OpUpdate); err != nil {
		return err
	}
	return b.inner.UpdateAll(ctx, entities)
}

func (b *batchWriter[T, ID]) DeleteAll(ctx context.Context, ids []ID) error {
	if err := b.app.require(ctx, authaction.OpDelete); err != nil {
		return err
	}
	return b.inner.DeleteAll(ctx, ids)
}

// auditedSurface 是带动作校验的审计扩展实现。
//
// Purge / Restore 是高危写操作，AuditTrail / ListDeleted 是能看到已删数据的读操作，
// 三类都有独立动作码，不复用基础 CRUD 的 delete / read。
type auditedSurface[T domain.IEntity[ID], ID comparable] struct {
	app   *application[T, ID]
	inner capability.IAuditedSurface[T, ID]
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

func (s *auditedSurface[T, ID]) AuditTrail(ctx context.Context, id ID, offset, limit int) ([]audited.AuditRecord, error) {
	if err := s.app.require(ctx, authaction.OpAuditRead); err != nil {
		return nil, err
	}
	return s.inner.AuditTrail(ctx, id, offset, limit)
}

// AuditStore 不是请求路径，原样暴露供 REST 能力探测使用。
func (s *auditedSurface[T, ID]) AuditStore() audited.IAuditStore { return s.inner.AuditStore() }
