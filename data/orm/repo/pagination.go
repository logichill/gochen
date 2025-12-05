package repo

import (
	"context"
	"math"

	"gochen/errors"
)

func (r *Repo[T]) ListPage(ctx context.Context, options *QueryOptions) (*PagedResult[T], error) {
	var entities []T
	var total int64

	q := r.query(ctx).Where("deleted_at IS NULL")
	if options.Filters != nil {
		q = q.withFilters(options.Filters)
	}
	if options.Advanced != nil {
		q = r.applyAdvancedFilters(q, options.Advanced)
	}

	total, err := q.Count()
	if err != nil {
		return nil, errors.WrapError(err, errors.ErrCodeDatabase, "failed to count total records")
	}

	q = r.applySorting(q, options)
	if len(options.Fields) > 0 {
		safeFields := make([]string, 0, len(options.Fields))
		for _, f := range options.Fields {
			if q.isAllowedField(f) {
				safeFields = append(safeFields, f)
			}
		}
		if len(safeFields) > 0 {
			q = q.Select(safeFields...)
		}
	}
	offset := (options.Page - 1) * options.Size
	q = q.Offset(offset).Limit(options.Size)

	if err := q.Find(&entities); err != nil {
		return nil, errors.WrapError(err, errors.ErrCodeDatabase, "failed to execute paginated query")
	}

	size := options.Size
	if size <= 0 {
		size = 1
	}
	totalPages := int(math.Ceil(float64(total) / float64(size)))
	result := make([]*T, len(entities))
	for i := range entities {
		result[i] = &entities[i]
	}

	return &PagedResult[T]{
		Data:       result,
		Total:      total,
		Page:       options.Page,
		Size:       size,
		TotalPages: totalPages,
	}, nil
}
