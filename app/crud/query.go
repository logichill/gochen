package crud

import (
	"context"
	"strings"

	"gochen/app/query"
	"gochen/errors"
)

// newQueryableRepositoryRequiredError 创建Queryable仓储Required错误。
func newQueryableRepositoryRequiredError() error {
	return errors.NewCode(errors.Unsupported, "query-based operations are not supported").
		WithContext("required_interface", "app/query.IQueryableRepository")
}

// normalizeSorts 规范化Sorts。
func normalizeSorts(in []query.Sort) ([]query.Sort, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]query.Sort, 0, len(in))
	for _, srt := range in {
		field := strings.TrimSpace(srt.Field)
		if field == "" {
			return nil, errors.NewCode(errors.InvalidInput, "sort field cannot be empty")
		}
		if !srt.Direction.IsValid() {
			return nil, errors.NewCode(errors.InvalidInput, "invalid sort direction").
				WithContext("field", field).
				WithContext("direction", string(srt.Direction))
		}
		out = append(out, query.Sort{Field: field, Direction: srt.Direction})
	}
	return out, nil
}

func normalizeQueryRequest(request *query.QueryRequest) (query.QueryRequest, error) {
	if request == nil {
		return query.QueryRequest{}, nil
	}
	sorts, err := normalizeSorts(request.Sorts)
	if err != nil {
		return query.QueryRequest{}, err
	}
	return query.QueryRequest{
		Fields:  append([]string(nil), request.Fields...),
		Sorts:   sorts,
		Filters: request.Filters.Clone(),
	}, nil
}

// ListByQuery 根据统一查询请求获取实体列表。
func (s *Application[T, ID]) ListByQuery(ctx context.Context, quer *query.QueryRequest) ([]T, error) {
	normalized, err := normalizeQueryRequest(quer)
	if err != nil {
		return nil, err
	}
	options := query.QueryOptions{
		Fields:  normalized.Fields,
		Sorts:   normalized.Sorts,
		Filters: normalized.Filters,
	}

	if queryableRepo, ok := s.Repository().(query.IQueryableRepository[T]); ok {
		return queryableRepo.Query(ctx, options)
	}

	if normalized.IsZero() {
		queryRepo, ok := s.QueryRepository()
		if !ok {
			return nil, newQueryableRepositoryRequiredError()
		}
		limit := s.maxPageSize()
		if limit <= 0 {
			limit = defaultMaxPageSize
		}
		return queryRepo.List(ctx, 0, limit)
	}

	return nil, newQueryableRepositoryRequiredError()
}

// ListPage 列出Page。
func (s *Application[T, ID]) ListPage(ctx context.Context, request *query.PaginationOptions) (*query.PagedResult[T], error) {
	if request == nil {
		request = &query.PaginationOptions{Page: 1, Size: 10}
	}

	if err := request.Validate(s.maxPageSize()); err != nil {
		return nil, errors.Wrap(err, errors.InvalidInput, "invalid pagination options")
	}

	quer, err := normalizeQueryRequest(&query.QueryRequest{
		Fields:  request.Fields,
		Sorts:   request.Sorts,
		Filters: request.Filters,
	})
	if err != nil {
		return nil, err
	}
	queryOpts := query.QueryOptions{
		Offset:   request.Offset(),
		Limit:    request.Size,
		Fields:   quer.Fields,
		Sorts:    quer.Sorts,
		Filters:  quer.Filters,
		Advanced: request.Advanced,
	}

	var data []T
	var total int64

	if queryableRepo, ok := s.Repository().(query.IQueryableRepository[T]); ok {
		data, err = queryableRepo.Query(ctx, queryOpts)
		if err != nil {
			return nil, err
		}

		countOpts := query.QueryOptions{
			Filters:  quer.Filters,
			Advanced: request.Advanced,
		}
		total, err = queryableRepo.QueryCount(ctx, countOpts)
		if err != nil {
			return nil, err
		}
	} else {
		if !quer.IsZero() || !request.Advanced.IsZero() {
			return nil, newQueryableRepositoryRequiredError()
		}
		queryRepo, ok := s.QueryRepository()
		if !ok {
			return nil, newQueryableRepositoryRequiredError()
		}

		data, err = queryRepo.List(ctx, queryOpts.Offset, queryOpts.Limit)
		if err != nil {
			return nil, err
		}

		total, err = queryRepo.Count(ctx)
		if err != nil {
			return nil, err
		}
	}

	totalPages := int((total + int64(request.Size) - 1) / int64(request.Size))
	hasNext := request.Page < totalPages
	hasPrev := request.Page > 1

	return &query.PagedResult[T]{
		Data:       data,
		Total:      total,
		Page:       request.Page,
		Size:       request.Size,
		TotalPages: totalPages,
		HasNext:    hasNext,
		HasPrev:    hasPrev,
	}, nil
}

// CountByQuery 按过滤条件统计实体数量。
func (s *Application[T, ID]) CountByQuery(ctx context.Context, quer *query.QueryRequest) (int64, error) {
	normalized, err := normalizeQueryRequest(quer)
	if err != nil {
		return 0, err
	}
	if normalized.Filters.IsZero() {
		queryRepo, ok := s.QueryRepository()
		if !ok {
			return 0, newQueryableRepositoryRequiredError()
		}
		return queryRepo.Count(ctx)
	}

	options := query.QueryOptions{
		Filters: normalized.Filters,
	}
	if queryableRepo, ok := s.Repository().(query.IQueryableRepository[T]); ok {
		return queryableRepo.QueryCount(ctx, options)
	}

	return 0, newQueryableRepositoryRequiredError()
}
