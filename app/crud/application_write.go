package crud

import "context"

// Create 创建单个实体。
func (a *Application[T, ID]) Create(ctx context.Context, entity T) error {
	return a.core.Create(ctx, entity)
}

// Update 更新单个实体；无业务字段变更时跳过写入与审计。
func (a *Application[T, ID]) Update(ctx context.Context, entity T) error {
	return a.core.Update(ctx, entity)
}

// Execute 修改已有实体。
func (a *Application[T, ID]) Execute(
	ctx context.Context,
	action string,
	id ID,
	fn func(ctx context.Context, e T) error,
	opts ...ExecOption,
) (T, error) {
	return a.core.Execute(ctx, action, id, fn, opts...)
}

// ExecuteNew 新建实体。
func (a *Application[T, ID]) ExecuteNew(
	ctx context.Context,
	action string,
	fn func(ctx context.Context) (T, error),
	opts ...ExecOption,
) (T, error) {
	return a.core.ExecuteNew(ctx, action, fn, opts...)
}

// ExecuteBulk 一次授权、一个事务修改多个实体。
func (a *Application[T, ID]) ExecuteBulk(
	ctx context.Context,
	action string,
	ids []ID,
	fn func(ctx context.Context, e T) error,
	opts ...ExecOption,
) ([]T, error) {
	return a.core.ExecuteBulk(ctx, action, ids, fn, opts...)
}

// ExecuteNewBulk 一次授权、一个事务新建多个实体。
func (a *Application[T, ID]) ExecuteNewBulk(
	ctx context.Context,
	action string,
	fn func(ctx context.Context) ([]T, error),
	opts ...ExecOption,
) ([]T, error) {
	return a.core.ExecuteNewBulk(ctx, action, fn, opts...)
}
