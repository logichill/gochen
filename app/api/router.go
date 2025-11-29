// Package api 提供 RESTful API 路由构建功能
package api

import (
	"fmt"
	"strconv"
	"strings"

	application "gochen/app/application"
	"gochen/domain/entity"
	"gochen/errors"
	core "gochen/http"
)

// IRouteBuilder 路由构建器接口
type IRouteBuilder[T entity.IEntity[int64]] interface {
	// 配置路由行为
	WithConfig(config *RouteConfig) IRouteBuilder[T]

	// 注册中间件
	Use(middlewares ...core.Middleware) IRouteBuilder[T]

	// 注册到路由组
	Register(group core.IRouteGroup) error
}

// RouteBuilder 路由构���器实现
type RouteBuilder[T entity.IEntity[int64]] struct {
	config      *RouteConfig
	middlewares []core.Middleware
	service     application.IApplication[T]
}

// NewRouteBuilder 创建路由构建器
func NewRouteBuilder[T entity.IEntity[int64]](svc application.IApplication[T]) IRouteBuilder[T] {
	return &RouteBuilder[T]{
		config:  DefaultRouteConfig(),
		service: svc,
	}
}

// WithConfig 配置路由行为
func (rb *RouteBuilder[T]) WithConfig(config *RouteConfig) IRouteBuilder[T] {
	if config != nil {
		rb.config = config
	}
	return rb
}

// Use 注册中间件
func (rb *RouteBuilder[T]) Use(middlewares ...core.Middleware) IRouteBuilder[T] {
	rb.middlewares = append(rb.middlewares, middlewares...)
	return rb
}

// Register 注册到路由组
func (rb *RouteBuilder[T]) Register(group core.IRouteGroup) error {
	if rb.service == nil {
		return fmt.Errorf("service cannot be nil")
	}

	// 注册路由
	rb.registerListRoutes(group)
	rb.registerEntityRoutes(group)

	if rb.config.EnableBatch {
		rb.registerBatchRoutes(group)
	}

	return nil
}

// registerListRoutes 注册列表相关路由
func (rb *RouteBuilder[T]) registerListRoutes(group core.IRouteGroup) {
	basePath := rb.config.BasePath
	if basePath == "" {
		basePath = ""
	}

	// GET /resource - 获取列表
	group.GET(basePath, rb.wrapHandler(rb.handleList))
}

// registerEntityRoutes 注册实体相关路由
func (rb *RouteBuilder[T]) registerEntityRoutes(group core.IRouteGroup) {
	basePath := rb.config.BasePath
	if basePath == "" {
		basePath = ""
	}

	// GET /resource/:id - 获取单个实体
	group.GET(fmt.Sprintf("%s/:id", basePath), rb.wrapHandler(rb.handleGet))

	// POST /resource - 创建实体
	group.POST(basePath, rb.wrapHandler(rb.handleCreate))

	// PUT /resource/:id - 更新实体
	group.PUT(fmt.Sprintf("%s/:id", basePath), rb.wrapHandler(rb.handleUpdate))

	// DELETE /resource/:id - 删除实体
	group.DELETE(fmt.Sprintf("%s/:id", basePath), rb.wrapHandler(rb.handleDelete))
}

// registerBatchRoutes 注册批量操作路由
func (rb *RouteBuilder[T]) registerBatchRoutes(group core.IRouteGroup) {
	basePath := rb.config.BasePath
	if basePath == "" {
		basePath = ""
	}

	// POST /resource/batch - 批量创建
	group.POST(fmt.Sprintf("%s/batch", basePath), rb.wrapHandler(rb.handleCreateBatch))

	// PUT /resource/batch - 批量更新
	group.PUT(fmt.Sprintf("%s/batch", basePath), rb.wrapHandler(rb.handleUpdateBatch))

	// DELETE /resource/batch - 批量删除
	group.DELETE(fmt.Sprintf("%s/batch", basePath), rb.wrapHandler(rb.handleDeleteBatch))
}

// wrapHandler 包装处理器，应用中间件和错误处理
func (rb *RouteBuilder[T]) wrapHandler(handler func(core.IHttpContext) error) func(core.IHttpContext) error {
	return func(c core.IHttpContext) error {
		middlewares := make([]core.Middleware, 0, len(rb.middlewares))
		middlewares = append(middlewares, rb.middlewares...)
		if rb.config != nil && len(rb.config.Middlewares) > 0 {
			middlewares = append(middlewares, rb.config.Middlewares...)
		}

		executor := handler
		for i := len(middlewares) - 1; i >= 0; i-- {
			mw := middlewares[i]
			next := executor
			executor = func(ctx core.IHttpContext) error {
				return mw(ctx, func() error {
					return next(ctx)
				})
			}
		}

		if err := executor(c); err != nil {
			var errorHandler func(error) core.IResponse
			if rb.config != nil {
				errorHandler = rb.config.ErrorHandler
			}
			if errorHandler == nil {
				errorHandler = DefaultErrorHandler
			}
			response := errorHandler(err)
			if response == nil {
				response = DefaultErrorHandler(err)
			}
			return response.Send(c)
		}

		return nil
	}
}

// 请求处理方法
func (rb *RouteBuilder[T]) handleList(c core.IHttpContext) error {
	if rb.config.EnablePagination {
		return rb.handlePagedList(c)
	}

	query := rb.parseQueryParams(c)
	result, err := rb.service.ListByQuery(c.GetContext(), query)
	if err != nil {
		return err
	}

	wrappedData := rb.config.ResponseWrapper(result)
	return c.JSON(200, wrappedData)
}

func (rb *RouteBuilder[T]) handlePagedList(c core.IHttpContext) error {
	options := rb.parsePaginationOptions(c)
	result, err := rb.service.ListPage(c.GetContext(), options)
	if err != nil {
		return err
	}

	wrappedData := rb.config.ResponseWrapper(result)
	return c.JSON(200, wrappedData)
}

func (rb *RouteBuilder[T]) handleGet(c core.IHttpContext) error {
	id, err := strconv.ParseInt(c.GetParam("id"), 10, 64)
	if err != nil {
		return errors.NewValidationError("无效的ID格式")
	}

	entity, err := rb.service.GetByID(c.GetContext(), id)
	if err != nil {
		return err
	}

	wrappedData := rb.config.ResponseWrapper(entity)
	return c.JSON(200, wrappedData)
}

func (rb *RouteBuilder[T]) handleCreate(c core.IHttpContext) error {
	var entity T
	if err := c.BindJSON(&entity); err != nil {
		return errors.NewValidationError("无效的请求数据")
	}

	// 执行自定义验证
	if rb.config.Validator != nil {
		if err := rb.config.Validator(entity); err != nil {
			return err
		}
	}

	if err := rb.service.Create(c.GetContext(), entity); err != nil {
		return err
	}

	result := map[string]any{
		"id": entity.GetID(),
	}
	wrappedData := rb.config.ResponseWrapper(result)
	// 为了与现有测试/调用方兼容，这里返回 200 而非 201
	return c.JSON(200, wrappedData)
}

func (rb *RouteBuilder[T]) handleUpdate(c core.IHttpContext) error {
	id, err := strconv.ParseInt(c.GetParam("id"), 10, 64)
	if err != nil {
		return errors.NewValidationError("无效的ID格式")
	}

	// 首先获取现有实体
	entity, err := rb.service.GetByID(c.GetContext(), id)
	if err != nil {
		return err
	}

	// 绑定请求数据到现有实体
	if err := c.BindJSON(&entity); err != nil {
		return errors.NewValidationError("无效的请求数据")
	}

	// 执行自定义验证
	if rb.config.Validator != nil {
		if err := rb.config.Validator(entity); err != nil {
			return err
		}
	}

	if err := rb.service.Update(c.GetContext(), entity); err != nil {
		return err
	}

	wrappedData := rb.config.ResponseWrapper(entity)
	return c.JSON(200, wrappedData)
}

func (rb *RouteBuilder[T]) handleDelete(c core.IHttpContext) error {
	id, err := strconv.ParseInt(c.GetParam("id"), 10, 64)
	if err != nil {
		return errors.NewValidationError("无效的ID格式")
	}

	if err := rb.service.Delete(c.GetContext(), id); err != nil {
		return err
	}

	wrappedData := rb.config.ResponseWrapper(nil)
	return c.JSON(200, wrappedData)
}

func (rb *RouteBuilder[T]) handleCreateBatch(c core.IHttpContext) error {
	var entities []T
	if err := c.BindJSON(&entities); err != nil {
		return errors.NewValidationError("无效的请求数据")
	}

	result, err := rb.service.CreateBatch(c.GetContext(), entities)
	if err != nil {
		return err
	}

	wrappedData := rb.config.ResponseWrapper(result)
	return c.JSON(201, wrappedData)
}

func (rb *RouteBuilder[T]) handleUpdateBatch(c core.IHttpContext) error {
	var entities []T
	if err := c.BindJSON(&entities); err != nil {
		return errors.NewValidationError("无效的请求数据")
	}

	result, err := rb.service.UpdateBatch(c.GetContext(), entities)
	if err != nil {
		return err
	}

	wrappedData := rb.config.ResponseWrapper(result)
	return c.JSON(200, wrappedData)
}

func (rb *RouteBuilder[T]) handleDeleteBatch(c core.IHttpContext) error {
	var req struct {
		IDs []int64 `json:"ids" binding:"required"`
	}

	if err := c.BindJSON(&req); err != nil {
		return errors.NewValidationError("无效的请求数据")
	}

	result, err := rb.service.DeleteBatch(c.GetContext(), req.IDs)
	if err != nil {
		return err
	}

	wrappedData := rb.config.ResponseWrapper(result)
	return c.JSON(200, wrappedData)
}

// 辅助方法
func (rb *RouteBuilder[T]) parseQueryParams(c core.IHttpContext) *application.QueryParams {
	query := &application.QueryParams{
		Filters: make(map[string]string),
		Sorts:   make(map[string]string),
	}

	// 解析过滤条件
	for k, values := range c.GetQueryParams() {
		if len(values) > 0 && !rb.isReservedParam(k) {
			query.Filters[k] = values[0]
		}
	}

	// 解析排序
	if sort := c.GetQuery("sort"); sort != "" {
		order := c.GetQuery("order")
		if order == "" {
			order = "asc"
		}
		query.Sorts[sort] = order
	}

	// 解析字段选择
	if fields := c.GetQuery("fields"); fields != "" {
		query.Fields = strings.Split(fields, ",")
	}

	return query
}

func (rb *RouteBuilder[T]) parsePaginationOptions(c core.IHttpContext) *application.PaginationOptions {
	options := &application.PaginationOptions{
		Page:    1,
		Size:    rb.config.DefaultPageSize,
		Sorts:   make(map[string]string),
		Filters: make(map[string]string),
	}

	// 解析页码
	if pageStr := c.GetQuery("page"); pageStr != "" {
		if page, err := strconv.Atoi(pageStr); err == nil && page > 0 {
			options.Page = page
		}
	}

	// 解析每页大小（兼容 page_size 别名）
	sizeStr := c.GetQuery("size")
	if sizeStr == "" {
		sizeStr = c.GetQuery("page_size")
	}
	if sizeStr != "" {
		if size, err := strconv.Atoi(sizeStr); err == nil && size > 0 {
			options.Size = size
		}
	}

	if rb.config != nil && rb.config.MaxPageSize > 0 && options.Size > rb.config.MaxPageSize {
		options.Size = rb.config.MaxPageSize
	}

	// 解析排序
	if sort := c.GetQuery("sort"); sort != "" {
		order := c.GetQuery("order")
		if order == "" {
			order = "asc"
		}
		options.Sorts[sort] = order
	}

	// 解析过滤条件
	for k, values := range c.GetQueryParams() {
		if len(values) > 0 && !rb.isReservedParam(k) {
			options.Filters[k] = values[0]
		}
	}

	// 解析字段选择
	if fields := c.GetQuery("fields"); fields != "" {
		options.Fields = strings.Split(fields, ",")
	}

	return options
}

func (rb *RouteBuilder[T]) isReservedParam(param string) bool {
	// 兼容 page_size 作为分页大小别名，防止误当作过滤字段
	reserved := []string{"page", "size", "page_size", "sort", "order", "fields"}
	for _, r := range reserved {
		if param == r {
			return true
		}
	}
	return false
}
