package repo

import (
	"context"
	"math"

	"gochen/db/query"
	"gochen/errors"
)

// ListPage 从存储中查询对象。
func (r *Repo[T, ID]) ListPage(ctx context.Context, request *query.PageRequest) (*query.PagedResult[T], error) {
	var entities []T
	var total int64

	if request == nil {
		request = &query.PageRequest{}
	}
	if err := request.Validate(0); err != nil {
		return nil, errors.Wrap(err, errors.InvalidInput, "invalid pagination options")
	}

	quer, err := r.query(ctx)
	if err != nil {
		return nil, err
	}
	if r.softDelete {
		quer = quer.Where(r.softDeleteCols.DeletedAt + " IS NULL")
	}
	if !request.Filters.IsZero() {
		quer = quer.withQueryFilters(request.Filters)
	}
	if !request.Advanced.IsZero() {
		quer = r.applyAdvancedFilters(quer, request.Advanced)
	}

	total, err = quer.Count()
	if err != nil {
		return nil, errors.Wrap(err, errors.Database, "failed to count total records")
	}

	quer = r.applySorting(quer, request)
	if len(request.Fields) > 0 {
		safeFields := make([]string, 0, len(request.Fields))
		for _, f := range request.Fields {
			if quer.isAllowedField(f) {
				safeFields = append(safeFields, f)
			}
		}
		if len(safeFields) > 0 {
			quer = quer.Select(safeFields...)
		}
	}
	quer = quer.Offset(request.Offset()).Limit(request.Size)

	if err := quer.Find(&entities); err != nil {
		return nil, errors.Wrap(err, errors.Database, "failed to execute paginated query")
	}

	size := request.Size
	if size <= 0 {
		size = 1
	}
	totalPages := int(math.Ceil(float64(total) / float64(size)))
	hasNext := request.Page < totalPages
	hasPrev := request.Page > 1

	return &query.PagedResult[T]{
		Data:       entities,
		Total:      total,
		Page:       request.Page,
		Size:       size,
		TotalPages: totalPages,
		HasNext:    hasNext,
		HasPrev:    hasPrev,
	}, nil
}
