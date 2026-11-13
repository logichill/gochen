package crud

import "context"

// Delete 按冻结配置软删，或进入完整 Purge 流程。
func (a *Application[T, ID]) Delete(ctx context.Context, id ID) error {
	return a.core.Delete(ctx, id)
}

// DeleteBulk 一次授权、一个事务按冻结配置删除多个实体。
func (a *Application[T, ID]) DeleteBulk(ctx context.Context, ids []ID) error {
	return a.core.DeleteBulk(ctx, ids)
}

// Restore 恢复已软删实体；未启用恢复能力时拒绝执行。
func (a *Application[T, ID]) Restore(ctx context.Context, id ID, opts ...ExecOption) (T, error) {
	return a.core.Restore(ctx, id, opts...)
}

// Purge 永久物理清除实体，包括已软删记录。
func (a *Application[T, ID]) Purge(ctx context.Context, id ID, opts ...ExecOption) error {
	return a.core.Purge(ctx, id, opts...)
}

// ListDeleted 分页读取已软删实体；未启用查询能力时拒绝执行。
func (a *Application[T, ID]) ListDeleted(ctx context.Context, offset, limit int) ([]T, error) {
	return a.core.ListDeleted(ctx, offset, limit)
}

// FastDeny 对业务删除解析实际动作后执行安全提前拒绝。
func (a *Application[T, ID]) FastDeny(ctx context.Context, action string) error {
	if action == ActionDelete {
		action = a.DeleteAction()
	}
	return a.core.FastDeny(ctx, action)
}

// DeleteAction 返回业务删除实际使用的冻结动作；硬模式为 purge。
func (a *Application[T, ID]) DeleteAction() string { return a.core.DeleteAction() }
