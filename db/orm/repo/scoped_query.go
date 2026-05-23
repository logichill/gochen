package repo

import (
	"context"

	"gochen/db/orm"
	"gochen/errors"
)

// ScopedQuery 暴露从统一 DataScope 入口构造出的查询能力，供下游 repo
// 在保留 tenant/scope 边界的前提下补充 preload/join 等领域查询形状。
type ScopedQuery struct {
	builder *queryBuilder
}

func newScopedQuery(builder *queryBuilder) *ScopedQuery {
	return &ScopedQuery{builder: builder}
}

func (quer *ScopedQuery) Where(expr string, args ...any) *ScopedQuery {
	quer.builder.Where(expr, args...)
	return quer
}

func (quer *ScopedQuery) Join(join orm.Join) *ScopedQuery {
	quer.builder.Join(join)
	return quer
}

func (quer *ScopedQuery) Preload(relations ...string) *ScopedQuery {
	quer.builder.Preload(relations...)
	return quer
}

func (quer *ScopedQuery) Order(column string, desc bool) *ScopedQuery {
	quer.builder.Order(column, desc)
	return quer
}

func (quer *ScopedQuery) Limit(limit int) *ScopedQuery {
	quer.builder.Limit(limit)
	return quer
}

func (quer *ScopedQuery) Offset(offset int) *ScopedQuery {
	quer.builder.Offset(offset)
	return quer
}

func (quer *ScopedQuery) Select(columns ...string) *ScopedQuery {
	quer.builder.Select(columns...)
	return quer
}

func (quer *ScopedQuery) GroupBy(columns ...string) *ScopedQuery {
	quer.builder.GroupBy(columns...)
	return quer
}

func (quer *ScopedQuery) ForUpdate() *ScopedQuery {
	quer.builder.ForUpdate()
	return quer
}

func (quer *ScopedQuery) First(dest any) error {
	return quer.builder.First(dest)
}

func (quer *ScopedQuery) Find(dest any) error {
	return quer.builder.Find(dest)
}

func (quer *ScopedQuery) Count() (int64, error) {
	return quer.builder.Count()
}

// ScopedQuery 创建一个默认已套用 DataScope 的查询入口。
func (r *Repo[T, ID]) ScopedQuery(ctx context.Context) (*ScopedQuery, error) {
	builder, err := r.query(ctx)
	if err != nil {
		return nil, err
	}
	query := newScopedQuery(builder)
	if r.softDelete {
		query.Where(r.softDeleteCols.DeletedAt + " IS NULL")
	}
	return query, nil
}

// GetWith 在统一 DataScope/soft-delete 边界上追加额外的查询形状后按主键读取。
func (r *Repo[T, ID]) GetWith(ctx context.Context, id ID, configure func(*ScopedQuery)) (T, error) {
	return r.FindOneWith(ctx, func(quer *ScopedQuery) {
		quer.Where("id = ?", id)
		if configure != nil {
			configure(quer)
		}
	})
}

// FindOneWith 在统一 DataScope/soft-delete 边界上追加额外查询条件，读取单条记录。
func (r *Repo[T, ID]) FindOneWith(ctx context.Context, configure func(*ScopedQuery)) (T, error) {
	var zero T
	quer, err := r.ScopedQuery(ctx)
	if err != nil {
		return zero, err
	}
	if configure != nil {
		configure(quer)
	}
	var entity T
	if err := quer.First(&entity); err != nil {
		if errors.Is(err, errors.NotFound) {
			return zero, errors.NewCode(errors.NotFound, "record not found")
		}
		return zero, errors.Wrap(err, errors.Database, "failed to query record")
	}
	return entity, nil
}
