package crud

import (
	"context"

	"gochen/db/query"
	domaincrud "gochen/domain/crud"
	"gochen/errors"
)

// TenantAwareWrapper 在应用层包装仓储，补齐 tenant 解析、注入与校验。
type TenantAwareWrapper[T domaincrud.ITenantEntity[ID], ID comparable] struct {
	inner     domaincrud.IRepository[T, ID]
	queryable query.IQueryableRepository[T, ID]
	resolver  ITenantResolver
}

// TenantAwareOption 定义 TenantAwareWrapper 的可选配置。
type TenantAwareOption[T domaincrud.ITenantEntity[ID], ID comparable] func(*TenantAwareWrapper[T, ID])

// WithTenantResolver 为租户包装器注入 tenant 解析策略。
func WithTenantResolver[T domaincrud.ITenantEntity[ID], ID comparable](resolver ITenantResolver) TenantAwareOption[T, ID] {
	return func(wrapper *TenantAwareWrapper[T, ID]) {
		if wrapper == nil {
			return
		}
		wrapper.resolver = resolver
	}
}

// NewTenantAwareWrapper 创建租户感知仓储包装器。
func NewTenantAwareWrapper[T domaincrud.ITenantEntity[ID], ID comparable](inner domaincrud.IRepository[T, ID], opts ...TenantAwareOption[T, ID]) *TenantAwareWrapper[T, ID] {
	wrapper := &TenantAwareWrapper[T, ID]{inner: inner}
	if queryable, ok := inner.(query.IQueryableRepository[T, ID]); ok && queryable != nil {
		wrapper.queryable = queryable
	}
	for _, opt := range opts {
		if opt != nil {
			opt(wrapper)
		}
	}
	return wrapper
}

// Create 创建实体，自动注入租户 ID。
func (w *TenantAwareWrapper[T, ID]) Create(ctx context.Context, e T) error {
	tenantID, err := w.requireTenantID(ctx)
	if err != nil {
		return err
	}
	e.SetTenantID(tenantID)
	return w.inner.Create(ctx, e)
}

// Update 更新实体，校验租户隔离。
func (w *TenantAwareWrapper[T, ID]) Update(ctx context.Context, e T) error {
	tenantID, err := w.requireTenantID(ctx)
	if err != nil {
		return err
	}

	stored, err := w.inner.Get(ctx, e.GetID())
	if err != nil {
		return err
	}
	if stored.GetTenantID() != tenantID {
		return errors.NewCode(errors.Forbidden, "cross-tenant update not allowed").
			WithContext("stored_tenant", stored.GetTenantID()).
			WithContext("ctx_tenant", tenantID)
	}
	if e.GetTenantID() != "" && e.GetTenantID() != tenantID {
		return errors.NewCode(errors.Forbidden, "cross-tenant update not allowed").
			WithContext("entity_tenant", e.GetTenantID()).
			WithContext("ctx_tenant", tenantID)
	}

	e.SetTenantID(tenantID)
	return w.inner.Update(ctx, e)
}

// Delete 删除实体，校验租户隔离。
func (w *TenantAwareWrapper[T, ID]) Delete(ctx context.Context, id ID) error {
	tenantID, err := w.requireTenantID(ctx)
	if err != nil {
		return err
	}

	entity, err := w.inner.Get(ctx, id)
	if err != nil {
		return err
	}
	if entity.GetTenantID() != tenantID {
		return errors.NewCode(errors.Forbidden, "cross-tenant delete not allowed").
			WithContext("entity_tenant", entity.GetTenantID()).
			WithContext("ctx_tenant", tenantID)
	}
	return w.inner.Delete(ctx, id)
}

// Get 获取实体，校验租户隔离。
func (w *TenantAwareWrapper[T, ID]) Get(ctx context.Context, id ID) (T, error) {
	tenantID, err := w.requireTenantID(ctx)
	if err != nil {
		var zero T
		return zero, err
	}

	entity, err := w.inner.Get(ctx, id)
	if err != nil {
		return entity, err
	}
	if entity.GetTenantID() != tenantID {
		var zero T
		return zero, errors.NewCode(errors.NotFound, "entity not found").
			WithContext("id", id)
	}
	return entity, nil
}

// List 分页查询当前租户的数据。
func (w *TenantAwareWrapper[T, ID]) List(ctx context.Context, offset, limit int) ([]T, error) {
	tenantID, err := w.requireTenantID(ctx)
	if err != nil {
		return nil, err
	}
	if w.queryable != nil {
		return w.queryable.Query(ctx, withTenantFilter(tenantID, query.QueryOptions{
			Offset: offset,
			Limit:  limit,
		}))
	}
	if repo, ok := w.inner.(domaincrud.ITenantRepository[T, ID]); ok && repo != nil {
		return repo.ListByTenant(ctx, tenantID, offset, limit)
	}
	return nil, errors.NewCode(errors.Unsupported, "tenant-aware list requires inner to implement ITenantRepository")
}

// Count 统计当前租户的数据总数。
func (w *TenantAwareWrapper[T, ID]) Count(ctx context.Context) (int64, error) {
	tenantID, err := w.requireTenantID(ctx)
	if err != nil {
		return 0, err
	}
	if w.queryable != nil {
		return w.queryable.QueryCount(ctx, withTenantFilter(tenantID, query.QueryOptions{}))
	}
	if repo, ok := w.inner.(domaincrud.ITenantRepository[T, ID]); ok && repo != nil {
		return repo.CountByTenant(ctx, tenantID)
	}
	return 0, errors.NewCode(errors.Unsupported, "tenant-aware count requires inner to implement ITenantRepository")
}

// Exists 检查实体是否存在于当前租户。
func (w *TenantAwareWrapper[T, ID]) Exists(ctx context.Context, id ID) (bool, error) {
	tenantID, err := w.requireTenantID(ctx)
	if err != nil {
		return false, err
	}

	entity, err := w.inner.Get(ctx, id)
	if err != nil {
		if errors.Is(err, errors.NotFound) {
			return false, nil
		}
		return false, err
	}
	return entity.GetTenantID() == tenantID, nil
}

func (w *TenantAwareWrapper[T, ID]) requireTenantID(ctx context.Context) (string, error) {
	if w != nil && w.resolver != nil {
		return w.resolver.ResolveTenantID(ctx)
	}
	return ResolveTenantID(ctx)
}

// Inner 返回底层仓储（用于少量需要绕过 tenant 包装的场景）。
func (w *TenantAwareWrapper[T, ID]) Inner() domaincrud.IRepository[T, ID] {
	return w.inner
}

// Query 按当前租户执行统一查询。
func (w *TenantAwareWrapper[T, ID]) Query(ctx context.Context, opts query.QueryOptions) ([]T, error) {
	if w == nil || w.queryable == nil {
		return nil, errors.NewCode(errors.Unsupported, "tenant-aware query requires inner to implement db/query.IQueryableRepository")
	}
	tenantID, err := w.requireTenantID(ctx)
	if err != nil {
		return nil, err
	}
	return w.queryable.Query(ctx, withTenantFilter(tenantID, opts))
}

// QueryOne 按当前租户执行单条查询。
func (w *TenantAwareWrapper[T, ID]) QueryOne(ctx context.Context, opts query.QueryOptions) (T, error) {
	if w == nil || w.queryable == nil {
		var zero T
		return zero, errors.NewCode(errors.Unsupported, "tenant-aware query requires inner to implement db/query.IQueryableRepository")
	}
	tenantID, err := w.requireTenantID(ctx)
	if err != nil {
		var zero T
		return zero, err
	}
	return w.queryable.QueryOne(ctx, withTenantFilter(tenantID, opts))
}

// QueryCount 按当前租户统计统一查询结果。
func (w *TenantAwareWrapper[T, ID]) QueryCount(ctx context.Context, opts query.QueryOptions) (int64, error) {
	if w == nil || w.queryable == nil {
		return 0, errors.NewCode(errors.Unsupported, "tenant-aware query requires inner to implement db/query.IQueryableRepository")
	}
	tenantID, err := w.requireTenantID(ctx)
	if err != nil {
		return 0, err
	}
	return w.queryable.QueryCount(ctx, withTenantFilter(tenantID, opts))
}

// WithinTx 在事务中执行 fn。
func (w *TenantAwareWrapper[T, ID]) WithinTx(ctx context.Context, fn func(txCtx context.Context) error) error {
	txRepo, ok := w.inner.(ITransactional)
	if !ok || txRepo == nil {
		return errors.NewCode(errors.Unsupported, "tenant-aware repository inner does not implement transactions")
	}
	return txRepo.WithinTx(ctx, fn)
}
