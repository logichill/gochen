package crud

import (
	"context"

	"gochen/app/query"

	"gochen/domain/spec"
)

// Get 读取单个实体。
func (a *readApplication[T, ID]) Get(ctx context.Context, id ID) (T, error) {
	return a.core.Get(ctx, id)
}

// Exists 判断实体是否存在。
func (a *readApplication[T, ID]) Exists(ctx context.Context, id ID) (bool, error) {
	return a.core.Exists(ctx, id)
}

// List 分页读取实体列表。
func (a *readApplication[T, ID]) List(ctx context.Context, offset, limit int) ([]T, error) {
	return a.core.List(ctx, offset, limit)
}

// Query 以统一查询请求读取实体列表。
func (a *readApplication[T, ID]) Query(ctx context.Context, opts query.QueryOptions) ([]T, error) {
	return a.core.Query(ctx, opts)
}

// Count 统计当前可见实体总数。
func (a *readApplication[T, ID]) Count(ctx context.Context) (int64, error) {
	return a.core.Count(ctx)
}

// QueryCount 统计符合查询条件的可见实体。
func (a *readApplication[T, ID]) QueryCount(ctx context.Context, opts query.QueryOptions) (int64, error) {
	return a.core.QueryCount(ctx, opts)
}

// Read 执行自定义读用例：Application 只负责授权并交出规约。
func (a *readApplication[T, ID]) Read(
	ctx context.Context,
	action string,
	fn func(ctx context.Context, s spec.Spec) error,
) error {
	return a.core.Read(ctx, action, fn)
}
