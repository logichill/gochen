package crud

import (
	"context"

	"gochen/db/query"
	domaincrud "gochen/domain/crud"
	"gochen/errors"
)

// TenantQueryOption 定义租户查询包装器的可选配置。
type TenantQueryOption[T domaincrud.ITenantEntity[ID], ID comparable] func(*TenantQueryWrapper[T, ID])

// TenantQueryWrapper 为统一查询协议补齐租户过滤。
//
// 说明：
// - 仅承载 db/query 相关读能力，避免 domain/crud 直接依赖查询 DSL；
// - 基础 CRUD 读写应通过 app/crud.TenantAwareWrapper 提供。
type TenantQueryWrapper[T domaincrud.ITenantEntity[ID], ID comparable] struct {
	inner    query.IQueryableRepository[T, ID]
	resolver ITenantResolver
}

// WithTenantQueryResolver 为租户查询包装器注入 tenant 解析策略。
func WithTenantQueryResolver[T domaincrud.ITenantEntity[ID], ID comparable](resolver ITenantResolver) TenantQueryOption[T, ID] {
	return func(wrapper *TenantQueryWrapper[T, ID]) {
		if wrapper == nil {
			return
		}
		wrapper.resolver = resolver
	}
}

// NewTenantQueryWrapper 创建租户查询包装器。
func NewTenantQueryWrapper[T domaincrud.ITenantEntity[ID], ID comparable](inner query.IQueryableRepository[T, ID], opts ...TenantQueryOption[T, ID]) *TenantQueryWrapper[T, ID] {
	if inner == nil {
		return nil
	}
	wrapper := &TenantQueryWrapper[T, ID]{inner: inner}
	for _, opt := range opts {
		if opt != nil {
			opt(wrapper)
		}
	}
	return wrapper
}

// List 按当前租户执行分页查询。
func (w *TenantQueryWrapper[T, ID]) List(ctx context.Context, offset, limit int) ([]T, error) {
	if w == nil || w.inner == nil {
		return nil, errors.NewCode(errors.InvalidInput, "tenant query wrapper is nil")
	}
	tenantID, err := w.requireTenantID(ctx)
	if err != nil {
		return nil, err
	}
	return w.inner.Query(ctx, withTenantFilter(tenantID, query.QueryOptions{
		Offset: offset,
		Limit:  limit,
	}))
}

// Count 按当前租户统计统一查询结果。
func (w *TenantQueryWrapper[T, ID]) Count(ctx context.Context) (int64, error) {
	if w == nil || w.inner == nil {
		return 0, errors.NewCode(errors.InvalidInput, "tenant query wrapper is nil")
	}
	tenantID, err := w.requireTenantID(ctx)
	if err != nil {
		return 0, err
	}
	return w.inner.QueryCount(ctx, withTenantFilter(tenantID, query.QueryOptions{}))
}

// Query 按当前租户执行统一查询。
func (w *TenantQueryWrapper[T, ID]) Query(ctx context.Context, opts query.QueryOptions) ([]T, error) {
	if w == nil || w.inner == nil {
		return nil, errors.NewCode(errors.InvalidInput, "tenant query wrapper is nil")
	}
	tenantID, err := w.requireTenantID(ctx)
	if err != nil {
		return nil, err
	}
	return w.inner.Query(ctx, withTenantFilter(tenantID, opts))
}

// QueryOne 按当前租户执行单条查询。
func (w *TenantQueryWrapper[T, ID]) QueryOne(ctx context.Context, opts query.QueryOptions) (T, error) {
	if w == nil || w.inner == nil {
		var zero T
		return zero, errors.NewCode(errors.InvalidInput, "tenant query wrapper is nil")
	}
	tenantID, err := w.requireTenantID(ctx)
	if err != nil {
		var zero T
		return zero, err
	}
	return w.inner.QueryOne(ctx, withTenantFilter(tenantID, opts))
}

// QueryCount 按当前租户统计统一查询结果。
func (w *TenantQueryWrapper[T, ID]) QueryCount(ctx context.Context, opts query.QueryOptions) (int64, error) {
	if w == nil || w.inner == nil {
		return 0, errors.NewCode(errors.InvalidInput, "tenant query wrapper is nil")
	}
	tenantID, err := w.requireTenantID(ctx)
	if err != nil {
		return 0, err
	}
	return w.inner.QueryCount(ctx, withTenantFilter(tenantID, opts))
}

func (w *TenantQueryWrapper[T, ID]) requireTenantID(ctx context.Context) (string, error) {
	if w != nil && w.resolver != nil {
		return w.resolver.ResolveTenantID(ctx)
	}
	return ResolveTenantID(ctx)
}

func withTenantFilter(tenantID string, opts query.QueryOptions) query.QueryOptions {
	cloned := query.QueryOptions{
		Offset:   opts.Offset,
		Limit:    opts.Limit,
		Fields:   append([]string(nil), opts.Fields...),
		Sorts:    append([]query.Sort(nil), opts.Sorts...),
		Filters:  opts.Filters.Clone(),
		Advanced: opts.Advanced,
	}
	if cloned.Filters == nil {
		cloned.Filters = query.QueryFilters{}
	}
	cloned.Filters["tenant_id"] = []query.QueryExpr{{
		Op:    query.FilterOpEq,
		Value: query.StringValue(tenantID),
	}}
	return cloned
}
