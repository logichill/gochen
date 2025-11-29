package application

import (
	"context"
	"errors"
	"fmt"

	"gochen/domain/entity"
	repo "gochen/domain/repository"
)

// QueryParams 查询参数
type QueryParams struct {
	Filters map[string]string `json:"filters"`
	Sorts   map[string]string `json:"sorts"`
	Fields  []string          `json:"fields"`
}

// PaginationOptions 分页选项
type PaginationOptions struct {
	Page    int               `json:"page"`
	Size    int               `json:"size"`
	Sorts   map[string]string `json:"sorts"`
	Filters map[string]string `json:"filters"`
	Fields  []string          `json:"fields"`
}

// Validate 调整并验证分页参数
func (p *PaginationOptions) Validate(maxSize int) error {
	if p.Page < 1 {
		p.Page = 1
	}

	if p.Size < 1 {
		p.Size = 10
	}

	if maxSize > 0 && p.Size > maxSize {
		p.Size = maxSize
	}

	return nil
}

// PagedResult 分页结果
type PagedResult[T entity.IEntity[int64]] struct {
	Data       []T   `json:"data"`
	Total      int64 `json:"total"`
	Page       int   `json:"page"`
	Size       int   `json:"size"`
	TotalPages int   `json:"total_pages"`
	HasNext    bool  `json:"has_next"`
	HasPrev    bool  `json:"has_prev"`
}

// ErrQueryableRepositoryRequired 表示当前仓储未实现 IQueryableRepository，因此不支持基于 QueryOptions 的查询/统计。
var ErrQueryableRepositoryRequired = errors.New("repository does not implement IQueryableRepository; query-based operations are not supported")

// ListByQuery 根据查询参数获取列表
func (s *Application[T]) ListByQuery(ctx context.Context, query *QueryParams) ([]T, error) {
	if query == nil {
		query = &QueryParams{}
	}

	// 构建查询选项
	options := &repo.QueryOptions{
		Filters: make(map[string]any),
	}

	// 转换过滤条件
	for k, v := range query.Filters {
		options.Filters[k] = v
	}

	// 转换排序条件
	if len(query.Sorts) > 0 {
		for field, direction := range query.Sorts {
			options.OrderBy = field
			options.OrderDesc = direction == "desc"
			break // 暂时只支持单字段排序
		}
	}

	// 执行查询
	if queryableRepo, ok := s.Repository().(repo.IQueryableRepository[T, int64]); ok {
		return queryableRepo.Query(ctx, *options)
	}

	// 如果仓储不支持 IQueryableRepository：
	// - 当没有任何过滤/排序条件时，可以回退到基础 List；
	// - 当存在过滤/排序条件时，直接返回错误，避免静默忽略查询条件。
	if (len(query.Filters) == 0) && (len(query.Sorts) == 0) {
		limit := s.config.MaxPageSize
		if limit <= 0 {
			limit = 1000
		}
		return s.Repository().List(ctx, 0, limit)
	}

	return nil, ErrQueryableRepositoryRequired
}

// ListPage 分页查询
func (s *Application[T]) ListPage(ctx context.Context, options *PaginationOptions) (*PagedResult[T], error) {
	if options == nil {
		options = &PaginationOptions{Page: 1, Size: 10}
	}

	// 验证分页参数（使用 MaxPageSize 限制单页大小）
	if err := options.Validate(s.config.MaxPageSize); err != nil {
		return nil, fmt.Errorf("invalid pagination options: %w", err)
	}

	// 构建查询选项
	queryOpts := &repo.QueryOptions{
		Offset:  (options.Page - 1) * options.Size,
		Limit:   options.Size,
		Filters: make(map[string]any),
	}

	// 转换过滤条件
	for k, v := range options.Filters {
		queryOpts.Filters[k] = v
	}

	// 转换排序条件
	if len(options.Sorts) > 0 {
		for field, direction := range options.Sorts {
			queryOpts.OrderBy = field
			queryOpts.OrderDesc = direction == "desc"
			break // 暂时只支持单字段排序
		}
	}

	var data []T
	var total int64
	var err error

	// 尝试使用可查询仓储
	if queryableRepo, ok := s.Repository().(repo.IQueryableRepository[T, int64]); ok {
		data, err = queryableRepo.Query(ctx, *queryOpts)
		if err != nil {
			return nil, err
		}

		total, err = queryableRepo.QueryCount(ctx, *queryOpts)
		if err != nil {
			return nil, err
		}
	} else {
		// 仓储不支持 IQueryableRepository 时，仅在不存在过滤/排序条件时回退到基础 List/Count；
		// 若存在过滤/排序条件，则返回错误，避免误导调用方。
		if len(options.Filters) > 0 || len(options.Sorts) > 0 {
			return nil, ErrQueryableRepositoryRequired
		}

		data, err = s.Repository().List(ctx, queryOpts.Offset, queryOpts.Limit)
		if err != nil {
			return nil, err
		}

		total, err = s.Repository().Count(ctx)
		if err != nil {
			return nil, err
		}
	}

	// 计算总页数和导航信息
	totalPages := int((total + int64(options.Size) - 1) / int64(options.Size))
	hasNext := options.Page < totalPages
	hasPrev := options.Page > 1

	return &PagedResult[T]{
		Data:       data,
		Total:      total,
		Page:       options.Page,
		Size:       options.Size,
		TotalPages: totalPages,
		HasNext:    hasNext,
		HasPrev:    hasPrev,
	}, nil
}

// CountByQuery 根据查询参数统计数量
func (s *Application[T]) CountByQuery(ctx context.Context, query *QueryParams) (int64, error) {
	if query == nil || len(query.Filters) == 0 {
		return s.Repository().Count(ctx)
	}

	// 构建查询选项
	options := &repo.QueryOptions{
		Filters: make(map[string]any),
	}

	// 转换过滤条件
	for k, v := range query.Filters {
		options.Filters[k] = v
	}

	// 尝试使用可查询仓储
	if queryableRepo, ok := s.Repository().(repo.IQueryableRepository[T, int64]); ok {
		return queryableRepo.QueryCount(ctx, *options)
	}

	// 当存在过滤条件但仓储不支持 IQueryableRepository 时，直接返回错误，
	// 避免将 Count(ctx) 误当作带查询条件的统计结果。
	return 0, ErrQueryableRepositoryRequired
}

