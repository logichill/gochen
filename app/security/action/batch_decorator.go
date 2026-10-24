package action

import (
	"context"

	authaction "gochen/auth/action"
	"gochen/domain"
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
