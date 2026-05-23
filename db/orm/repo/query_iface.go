package repo

import (
	"context"
	"strings"

	"gochen/db/query"
	"gochen/errors"
)

func applyQueryFilters(quer *queryBuilder, opts query.QueryOptions) *queryBuilder {
	if !opts.Filters.IsZero() {
		quer = quer.withQueryFilters(opts.Filters)
	}
	return quer
}

func (r *Repo[T, ID]) applyAdvancedQueryFilters(quer *queryBuilder, opts query.QueryOptions) *queryBuilder {
	if !opts.Advanced.IsZero() {
		quer = r.applyAdvancedFilters(quer, opts.Advanced)
	}
	return quer
}

// applySelectFields 应用Select字段集合。
func (r *Repo[T, ID]) applySelectFields(quer *queryBuilder, fields []string) *queryBuilder {
	if len(fields) == 0 {
		return quer
	}
	allowed := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if quer.isAllowedField(f) {
			allowed = append(allowed, f)
		}
	}
	if len(allowed) == 0 {
		return quer
	}
	return quer.Select(allowed...)
}

// Query 从存储中查询对象。
//
// 说明：
// - Query 执行通用查询（实现 db/query.IQueryableRepository）
func (r *Repo[T, ID]) Query(ctx context.Context, opts query.QueryOptions) ([]T, error) {
	var entities []T
	quer, err := r.query(ctx)
	if err != nil {
		return nil, err
	}
	if r.softDelete {
		quer = quer.Where(r.softDeleteCols.DeletedAt + " IS NULL")
	}
	quer = r.applySelectFields(quer, opts.Fields)
	quer = applyQueryFilters(quer, opts)
	quer = r.applyAdvancedQueryFilters(quer, opts)
	if len(opts.Sorts) > 0 {
		for _, s := range opts.Sorts {
			field := strings.TrimSpace(s.Field)
			if field == "" || !s.Direction.IsValid() {
				continue
			}
			if !quer.isAllowedField(field) {
				return nil, errors.NewCode(errors.InvalidInput, "invalid order by field").
					WithContext("field", field)
			}
			quer = quer.Order(field, s.Direction == query.DESC)
		}
	}
	if opts.Offset > 0 {
		quer = quer.Offset(opts.Offset)
	}
	if opts.Limit > 0 {
		quer = quer.Limit(opts.Limit)
	}
	if err := quer.Find(&entities); err != nil {
		return nil, errors.Wrap(err, errors.Database, "failed to execute query")
	}
	return entities, nil
}

// QueryOne 从存储中查询对象。
//
// 说明：
// - QueryOne 查询单条记录（实现 db/query.IQueryableRepository）
func (r *Repo[T, ID]) QueryOne(ctx context.Context, opts query.QueryOptions) (T, error) {
	var entity T
	quer, err := r.query(ctx)
	if err != nil {
		var zero T
		return zero, err
	}
	if r.softDelete {
		quer = quer.Where(r.softDeleteCols.DeletedAt + " IS NULL")
	}
	quer = r.applySelectFields(quer, opts.Fields)
	quer = applyQueryFilters(quer, opts)
	quer = r.applyAdvancedQueryFilters(quer, opts)
	if len(opts.Sorts) > 0 {
		for _, s := range opts.Sorts {
			field := strings.TrimSpace(s.Field)
			if field == "" || !s.Direction.IsValid() {
				continue
			}
			if !quer.isAllowedField(field) {
				var zero T
				return zero, errors.NewCode(errors.InvalidInput, "invalid order by field").
					WithContext("field", field)
			}
			quer = quer.Order(field, s.Direction == query.DESC)
		}
	}
	err = quer.First(&entity)
	var zero T
	if err != nil {
		if errors.Is(err, errors.NotFound) {
			return zero, errors.NewCode(errors.NotFound, "record not found")
		}
		return zero, errors.Wrap(err, errors.Database, "failed to query single record")
	}
	return entity, nil
}

// QueryCount 从存储中查询对象。
//
// 说明：
// - QueryCount 查询数量（实现 db/query.IQueryableRepository）
func (r *Repo[T, ID]) QueryCount(ctx context.Context, opts query.QueryOptions) (int64, error) {
	quer, err := r.query(ctx)
	if err != nil {
		return 0, err
	}
	if r.softDelete {
		quer = quer.Where(r.softDeleteCols.DeletedAt + " IS NULL")
	}
	quer = applyQueryFilters(quer, opts)
	quer = r.applyAdvancedQueryFilters(quer, opts)
	count, err := quer.Count()
	if err != nil {
		return 0, errors.Wrap(err, errors.Database, "failed to count records")
	}
	return count, nil
}
