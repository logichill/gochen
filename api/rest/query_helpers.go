package rest

import (
	"gochen/db/query"
	"gochen/httpx"
)

// NewQueryRouteConfig 创建适用于手写路由的 QuerySchema 解析配置。
func NewQueryRouteConfig[ID comparable](schema *query.QuerySchema, defaultPageSize, maxPageSize int) *RouteConfig[ID] {
	cfg := DefaultRouteConfig[ID]()
	cfg.Query.QuerySchema = schema
	if defaultPageSize > 0 {
		cfg.Query.DefaultPageSize = defaultPageSize
	}
	if maxPageSize > 0 {
		cfg.Query.MaxPageSize = maxPageSize
	}
	return cfg
}

// ParseQueryParams 解析通用查询参数（filter/sorts/fields）。
func ParseQueryParams[ID comparable](c httpx.IContext, cfg *RouteConfig[ID]) (*query.QueryRequest, error) {
	return parseQueryParams(c, cfg)
}

// ParsePaginationOptions 解析分页查询参数（page/size/filter/sorts/fields）。
func ParsePaginationOptions[ID comparable](c httpx.IContext, cfg *RouteConfig[ID]) (*query.PaginationOptions, error) {
	return parsePaginationOptions(c, cfg)
}

// parseQueryParams 解析查询参数。
func (rb *RouteBuilder[T, ID]) parseQueryParams(c httpx.IContext) (*query.QueryRequest, error) {
	return parseQueryParams(c, rb.config)
}

// parsePaginationOptions 解析分页参数。
func (rb *RouteBuilder[T, ID]) parsePaginationOptions(c httpx.IContext) (*query.PaginationOptions, error) {
	return parsePaginationOptions(c, rb.config)
}

// AppendPaginationFilters 追加固定过滤条件，并同步补齐解码结果。
func AppendPaginationFilters(
	opts *query.PaginationOptions,
	schema *query.QuerySchema,
	filters ...query.Filter,
) error {
	if opts == nil || len(filters) == 0 {
		return nil
	}
	normalized, err := normalizeQueryFilters(schema, filters)
	if err != nil {
		return err
	}
	opts.Filters = opts.Filters.Merge(normalized)
	return nil
}

// AppendQueryFilters 追加固定过滤条件，并同步补齐解码结果。
func AppendQueryFilters(
	params *query.QueryRequest,
	schema *query.QuerySchema,
	filters ...query.Filter,
) error {
	if params == nil || len(filters) == 0 {
		return nil
	}
	normalized, err := normalizeQueryFilters(schema, filters)
	if err != nil {
		return err
	}
	params.Filters = params.Filters.Merge(normalized)
	return nil
}

func normalizeQueryFilters(
	schema *query.QuerySchema,
	filters []query.Filter,
) (query.QueryFilters, error) {
	if len(filters) == 0 {
		return nil, nil
	}
	if schema == nil {
		return query.DecodeAdapterFilters(filters), nil
	}
	normalized, err := schema.NormalizeFilters(filters)
	if err != nil {
		return nil, err
	}
	return schema.DecodeFilters(normalized)
}
