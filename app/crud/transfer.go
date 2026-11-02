package crud

import (
	"context"
	"gochen/auth"
	"gochen/domain"
)

// ITransferApplication 是可选的归属迁移入口，基础 CRUD 接口保持原有能力。
type ITransferApplication[T domain.IEntity[ID], ID comparable] interface {
	Transfer(context.Context, string, ID, uint64, []auth.Assignment, ...ExecOption) (T, error)
}

// Transfer 执行独立迁移动作，目标意图经两阶段授权后写入。
func (a *Application[T, ID]) Transfer(ctx context.Context, action string, id ID, expectedVersion uint64, targets []auth.Assignment, opts ...ExecOption) (T, error) {
	return a.core.Transfer(ctx, action, id, expectedVersion, targets, opts...)
}
