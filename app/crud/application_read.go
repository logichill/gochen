package crud

import (
	"context"
	"strings"

	"gochen/app/query"
	"gochen/domain/crud"
	"gochen/errors"
)

// Get 根据 ID 获取实体。
func (s *Application[T, ID]) Get(ctx context.Context, id ID) (T, error) {
	return s.repository.Get(ctx, id)
}

// List 返回实体列表，支持分页。
//
// 入参规范化与 ListPage 使用同一套分页约束：负数下标不透传（避免 SQL 语法错误），
// limit 按 ServiceConfig.MaxPageSize 封顶，防止误用的巨型 limit 触发全表加载 OOM。
func (s *Application[T, ID]) List(ctx context.Context, offset, limit int) ([]T, error) {
	repo, ok := s.QueryRepository()
	if !ok {
		return nil, errors.NewCode(errors.Unsupported, "list requires repository to implement crud.IQueryRepository")
	}
	offset, limit = NormalizePageBounds(offset, limit, s.maxPageSize())
	return repo.List(ctx, offset, limit)
}

// NormalizePageBounds 规范化分页参数：负 offset 归零；limit 规则形状与
// ListPage/PaginationOptions.Validate 相同（<1 回退默认、超过 max 封顶），
// 但默认值不同：这里回退 defaultMaxPageSize=1000，而 PaginationOptions.Validate
// 回退 10——两者口径分叉为已知未修项（审计 P2#3 4e3d67ea，已列入发行说明
// 未修清单，统一口径需作者做行为变更决策）。
func NormalizePageBounds(offset, limit, max int) (int, int) {
	if offset < 0 {
		offset = 0
	}
	if limit < 1 {
		limit = defaultMaxPageSize
	}
	if max > 0 && limit > max {
		limit = max
	}
	return offset, limit
}

// Count 返回当前实体的总记录数。
func (s *Application[T, ID]) Count(ctx context.Context) (int64, error) {
	repo, ok := s.QueryRepository()
	if !ok {
		return 0, errors.NewCode(errors.Unsupported, "count requires repository to implement crud.IQueryRepository")
	}
	return repo.Count(ctx)
}

// Exists 判断指定 ID 的实体是否存在。
func (s *Application[T, ID]) Exists(ctx context.Context, id ID) (bool, error) {
	repo, ok := s.QueryRepository()
	if !ok {
		return false, errors.NewCode(errors.Unsupported, "exists requires repository to implement crud.IQueryRepository")
	}
	return repo.Exists(ctx, id)
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
	options := query.QueryOptions{
		Filters: normalized.Filters,
	}
	if queryableRepo, ok := s.Repository().(query.IQueryableRepository[T]); ok {
		return queryableRepo.QueryCount(ctx, options)
	}
	if normalized.Filters.IsZero() {
		if queryRepo, ok := s.QueryRepository(); ok {
			return queryRepo.Count(ctx)
		}
	}

	return 0, newQueryableRepositoryRequiredError()
}

// QueryRepository 返回底层查询仓储扩展（若支持）。
func (s *Application[T, ID]) QueryRepository() (crud.IQueryRepository[T, ID], bool) {
	repo, ok := s.repository.(crud.IQueryRepository[T, ID])
	return repo, ok
}

// HasQueryCapability 报告底层仓储是否支持列表与计数查询能力（IQueryRepository 或 IQueryableRepository）。
func (s *Application[T, ID]) HasQueryCapability() bool {
	if s == nil || s.repository == nil {
		return false
	}
	if _, ok := s.repository.(crud.IQueryRepository[T, ID]); ok {
		return true
	}
	_, ok := s.repository.(query.IQueryableRepository[T])
	return ok
}

// HasQueryableCapability 报告底层仓储是否支持通用条件/排序/字段投影查询能力（IQueryableRepository）。
func (s *Application[T, ID]) HasQueryableCapability() bool {
	if s == nil || s.repository == nil {
		return false
	}
	_, ok := s.repository.(query.IQueryableRepository[T])
	return ok
}

// MaxPageSize 返回当前服务配置的单页最大记录数（已归一化，>0）。
func (s *Application[T, ID]) MaxPageSize() int {
	return s.maxPageSize()
}

func (s *Application[T, ID]) maxPageSize() int {
	cfg := s.serviceConfig()
	return cfg.MaxPageSize
}

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
