package execution

import (
	"context"

	"gochen/app/query"
	"gochen/auth"
	"gochen/domain/spec"
	"gochen/errors"
	"gochen/internal/depcheck"
)

// Get 读取单个实体。授权请求携带目标 ID，仓储按规约过滤。
func (c *Core[T, ID]) Get(ctx context.Context, id ID) (T, error) {
	var zero T
	if err := checkContext(ctx); err != nil {
		return zero, err
	}
	spec, _, err := c.authorizeKind(ctx, ActionRead, []ID{id}, auth.KindRead)
	if err != nil {
		return zero, err
	}
	return c.read.Get(ctx, id, spec)
}

// Exists 判断实体是否存在；不可见（不存在、已软删或被规约过滤）返回 false。
func (c *Core[T, ID]) Exists(ctx context.Context, id ID) (bool, error) {
	if err := checkContext(ctx); err != nil {
		return false, err
	}
	spec, _, err := c.authorizeKind(ctx, ActionRead, []ID{id}, auth.KindRead)
	if err != nil {
		return false, err
	}
	if _, err := c.read.Get(ctx, id, spec); err != nil {
		if errors.Is(err, errors.NotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// List 分页读取实体列表。
func (c *Core[T, ID]) List(ctx context.Context, offset, limit int) ([]T, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	spec, _, err := c.authorizeKind(ctx, ActionList, nil, auth.KindList)
	if err != nil {
		return nil, err
	}
	offset, limit = c.normalizePage(offset, limit)
	return c.read.Query(ctx, query.QueryOptions{Offset: offset, Limit: limit}, spec)
}

// Query 以统一查询请求读取实体列表。
func (c *Core[T, ID]) Query(ctx context.Context, opts query.QueryOptions) ([]T, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	spec, _, err := c.authorizeKind(ctx, ActionList, nil, auth.KindList)
	if err != nil {
		return nil, err
	}
	opts.Offset, opts.Limit = c.normalizePage(opts.Offset, opts.Limit)
	return c.read.Query(ctx, opts, spec)
}

// Count 统计当前可见实体的总数。
func (c *Core[T, ID]) Count(ctx context.Context) (int64, error) {
	return c.QueryCount(ctx, query.QueryOptions{})
}

// QueryCount 统计符合查询条件的可见实体，忽略分页、排序和投影。
func (c *Core[T, ID]) QueryCount(ctx context.Context, opts query.QueryOptions) (int64, error) {
	if err := checkContext(ctx); err != nil {
		return 0, err
	}
	spec, _, err := c.authorizeKind(ctx, ActionList, nil, auth.KindList)
	if err != nil {
		return 0, err
	}
	return c.read.QueryCount(ctx, query.QueryOptions{Filters: opts.Filters, Advanced: opts.Advanced}, spec)
}

// Read 执行自定义读用例：只负责授权并交出规约，查询由调用方完成。
func (c *Core[T, ID]) Read(ctx context.Context, action string, fn func(ctx context.Context, s spec.Spec) error) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if fn == nil {
		return errors.NewCode(errors.InvalidInput, "read function cannot be nil")
	}
	spec, _, err := c.authorizeKind(ctx, action, nil, auth.KindList)
	if err != nil {
		return err
	}
	return fn(ctx, spec)
}

// ListDeleted 分页读取已软删实体；普通 List 不可见已删记录。
func (c *Core[T, ID]) ListDeleted(ctx context.Context, offset, limit int) ([]T, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if depcheck.IsNil(c.deleted) {
		return nil, errors.NewCode(errors.Unsupported, "deleted entity query is not configured")
	}
	spec, _, err := c.authorizeKind(ctx, ActionListDeleted, nil, auth.KindList)
	if err != nil {
		return nil, err
	}
	offset, limit = c.normalizePage(offset, limit)
	return c.deleted.ListDeleted(ctx, offset, limit, spec)
}

func checkContext(ctx context.Context) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	return nil
}
