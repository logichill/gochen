// Package rest 提供 RESTful API 路由构建功能。
package rest

import (
	"fmt"

	"gochen/domain"
	"gochen/domain/audited"
	"gochen/domain/crud"
	"gochen/errors"
	"gochen/httpx"
)

// IRouteBuilder 路由构建器接口。
type IRouteBuilder[T domain.IEntity[ID], ID comparable] interface {
	// 配置路由行为
	WithConfig(config *RouteConfig[ID]) IRouteBuilder[T, ID]

	// 注册中间件
	Use(middlewares ...httpx.Middleware) IRouteBuilder[T, ID]

	// 注册到路由组
	Register(group httpx.IRouteGroup) error
}

// RouteBuilder 路由构建器实现。
type RouteBuilder[T domain.IEntity[ID], ID comparable] struct {
	config      *RouteConfig[ID]
	middlewares []httpx.Middleware
	service     any

	// auditedEnabled 表示当前实体类型是否为 audited（由 T 的方法集决定）。
	// 注意：audited 能力闭环仍需通过 service 能力校验（见 Register）。
	auditedEnabled bool
	auditedService IAuditedService[T, ID]
}

// NewRouteBuilder 创建路由构建器。
func NewRouteBuilder[T domain.IEntity[ID], ID comparable](svc any) IRouteBuilder[T, ID] {
	rb := &RouteBuilder[T, ID]{
		config:         DefaultRouteConfig[ID](),
		service:        svc,
		auditedEnabled: isAuditedEntityType[T, ID](),
	}
	if rb.auditedEnabled && !isNilService(svc) {
		if as, ok := svc.(IAuditedService[T, ID]); ok {
			rb.auditedService = as
		}
	}
	return rb
}

// WithConfig 配置路由行为。
func (rb *RouteBuilder[T, ID]) WithConfig(config *RouteConfig[ID]) IRouteBuilder[T, ID] {
	if config != nil {
		rb.config = config
	}
	return rb
}

// Use 注册中间件。
func (rb *RouteBuilder[T, ID]) Use(middlewares ...httpx.Middleware) IRouteBuilder[T, ID] {
	rb.middlewares = append(rb.middlewares, middlewares...)
	return rb
}

// Register 注册能力。
func (rb *RouteBuilder[T, ID]) Register(group httpx.IRouteGroup) error {
	if isNilService(rb.service) {
		return errors.NewCode(errors.InvalidInput, "service cannot be nil")
	}
	if rb.config == nil || rb.config.Routing.IDCodec == nil {
		return errors.NewCode(errors.InvalidInput, "RouteConfig.Routing.IDCodec is required")
	}
	if err := ensureQuerySchemaForType[T](rb.config); err != nil {
		return err
	}
	if err := rb.validateAuthorizationConfig(); err != nil {
		return err
	}
	if err := rb.validateServiceCapabilities(); err != nil {
		return err
	}
	if rb.hasEnabledBatchRoutes() && rb.requiresPlainBatchWriter() {
		if _, ok := rb.batchWriter(); !ok {
			return errors.NewCode(errors.InvalidInput, "batch routes require a built-in batch writer or service-specific batch writer")
		}
	}

	if err := rb.validateAuditedCapabilities(); err != nil {
		return err
	}

	// 注册路由
	if rb.config.Routing.EnableList || rb.auditedEnabled {
		rb.registerListRoutes(group)
	}
	rb.registerEntityRoutes(group)

	if rb.hasEnabledBatchRoutes() {
		rb.registerBatchRoutes(group)
	}

	if rb.auditedEnabled {
		rb.registerAuditedRoutes(group)
	}

	return nil
}

func (rb *RouteBuilder[T, ID]) validateServiceCapabilities() error {
	if rb.config.Routing.EnableList {
		if rb.config.Query.EnablePagination {
			if _, ok := rb.pagedListService(); !ok {
				return errors.NewCode(errors.InvalidInput, "list route with pagination requires service to implement ListPage")
			}
		} else if _, ok := rb.listService(); !ok {
			return errors.NewCode(errors.InvalidInput, "list route requires service to implement ListByQuery")
		}
	}
	if rb.config.Routing.EnableGet {
		if _, ok := rb.getService(); !ok {
			return errors.NewCode(errors.InvalidInput, "get route requires service to implement Get")
		}
	}
	if rb.config.Routing.EnableCreate {
		if _, ok := rb.createService(); !ok {
			return errors.NewCode(errors.InvalidInput, "create route requires service to implement Create")
		}
	}
	if rb.config.Routing.EnableUpdate {
		if _, ok := rb.getService(); !ok {
			return errors.NewCode(errors.InvalidInput, "update route requires service to implement Get")
		}
		if _, ok := rb.updateService(); !ok {
			return errors.NewCode(errors.InvalidInput, "update route requires service to implement Update")
		}
	}
	if rb.config.Routing.EnableDelete {
		if _, ok := rb.deleteService(); !ok {
			return errors.NewCode(errors.InvalidInput, "delete route requires service to implement Delete")
		}
		if rb.deletePermission() != "" {
			if _, ok := rb.resourceBoundaryRepository(); !ok {
				if _, ok := rb.getService(); !ok {
					return errors.NewCode(errors.InvalidInput, "authz-enabled delete route requires service to implement Get or resource boundary lookup")
				}
			}
		}
	}
	if rb.hasEnabledBatchRoutes() && rb.requiresPlainBatchWriter() {
		if _, ok := rb.batchWriter(); !ok {
			return errors.NewCode(errors.InvalidInput, "batch routes require a built-in batch writer or service-specific batch writer")
		}
	}
	return nil
}

func (rb *RouteBuilder[T, ID]) validateAuditedCapabilities() error {
	if !rb.auditedEnabled {
		return nil
	}
	if rb.config == nil || rb.config.Audit.OperatorExtractor == nil {
		return errors.NewCode(errors.InvalidInput, "audited entity requires RouteConfig.Audit.OperatorExtractor")
	}
	if rb.auditedService == nil {
		return errors.NewCode(errors.InvalidInput, "audited entity requires service to implement audited operations")
	}
	if rb.auditedService.AuditStore() == nil {
		return errors.NewCode(errors.InvalidInput, "audited entity requires non-nil auditStore")
	}
	return nil
}

func (rb *RouteBuilder[T, ID]) hasEnabledBatchRoutes() bool {
	return rb.config != nil && rb.config.Routing.EnableBatch &&
		(rb.config.Routing.EnableCreate || rb.config.Routing.EnableUpdate || rb.config.Routing.EnableDelete)
}

// registerListRoutes 注册列表相关路由。
func (rb *RouteBuilder[T, ID]) registerListRoutes(group httpx.IRouteGroup) {
	basePath := ""
	if rb.config != nil {
		basePath = rb.config.Routing.BasePath
	}

	if rb.config.Routing.EnableList {
		// GET /resource - 获取列表
		group.GET(basePath, rb.wrapHandler(rb.handleList))
	}

	// audited 扩展：GET /resource/deleted - 获取已删除列表
	if rb.auditedEnabled {
		group.GET(fmt.Sprintf("%s/deleted", basePath), rb.wrapHandler(rb.handleListDeleted))
	}
}

// registerEntityRoutes 注册实体相关路由。
func (rb *RouteBuilder[T, ID]) registerEntityRoutes(group httpx.IRouteGroup) {
	basePath := ""
	if rb.config != nil {
		basePath = rb.config.Routing.BasePath
	}

	if rb.config.Routing.EnableGet {
		group.GET(fmt.Sprintf("%s/:id", basePath), rb.wrapHandler(rb.handleGet))
	}

	if rb.config.Routing.EnableCreate {
		group.POST(basePath, rb.wrapHandler(rb.handleCreate))
	}

	if rb.config.Routing.EnableUpdate {
		group.PUT(fmt.Sprintf("%s/:id", basePath), rb.wrapHandler(rb.handleUpdate))
	}

	if rb.config.Routing.EnableDelete {
		group.DELETE(fmt.Sprintf("%s/:id", basePath), rb.wrapHandler(rb.handleDelete))
	}
}

// isAuditedEntityType 判断AuditedEntity类型。
func isAuditedEntityType[T domain.IEntity[ID], ID comparable]() bool {
	var zero T
	_, ok := any(zero).(audited.IAuditedEntity[ID])
	return ok
}

// registerAuditedRoutes 注册Audited路由集合。
func (rb *RouteBuilder[T, ID]) registerAuditedRoutes(group httpx.IRouteGroup) {
	basePath := ""
	if rb.config != nil {
		basePath = rb.config.Routing.BasePath
	}
	group.GET(fmt.Sprintf("%s/:id/audit", basePath), rb.wrapHandler(rb.handleAuditTrail))
	group.POST(fmt.Sprintf("%s/:id/restore", basePath), rb.wrapHandler(rb.handleRestore))

	// 物理删除（purge）为"危险操作"，仅在仓储或自定义 service 明确支持时才注册端点。
	if rb.auditedService != nil {
		if provider, ok := rb.repositoryProvider(); ok {
			if _, ok := provider.Repository().(crud.IPurgeRepository[T, ID]); ok {
				group.DELETE(fmt.Sprintf("%s/:id/purge", basePath), rb.wrapHandler(rb.handlePurge))
			}
			return
		}
		group.DELETE(fmt.Sprintf("%s/:id/purge", basePath), rb.wrapHandler(rb.handlePurge))
	}
}

// registerBatchRoutes 注册批量操作路由。
func (rb *RouteBuilder[T, ID]) registerBatchRoutes(group httpx.IRouteGroup) {
	basePath := ""
	if rb.config != nil {
		basePath = rb.config.Routing.BasePath
	}

	if rb.config.Routing.EnableCreate {
		group.POST(fmt.Sprintf("%s/batch", basePath), rb.wrapHandler(rb.handleCreateAll))
	}

	if rb.config.Routing.EnableUpdate {
		group.PUT(fmt.Sprintf("%s/batch", basePath), rb.wrapHandler(rb.handleUpdateBatch))
	}

	if rb.config.Routing.EnableDelete {
		group.DELETE(fmt.Sprintf("%s/batch", basePath), rb.wrapHandler(rb.handleDeleteBatch))
	}
}

// wrapHandler 包装处理器，应用中间件和错误处理。
//
// 说明：
// - 中间件链在调用时构建（而非每次请求），确保闭包正确捕获。
func (rb *RouteBuilder[T, ID]) wrapHandler(handler func(httpx.IContext) error) func(httpx.IContext) error {
	// 预构建中间件链（在路由注册时执行，而非每次请求）
	middlewares := make([]httpx.Middleware, 0, len(rb.middlewares)+len(rb.config.HTTP.Middlewares))
	middlewares = append(middlewares, rb.middlewares...)
	if rb.config != nil && len(rb.config.HTTP.Middlewares) > 0 {
		middlewares = append(middlewares, rb.config.HTTP.Middlewares...)
	}

	// 构建执行链：从后向前包装，确保执行顺序正确
	executor := handler
	for i := len(middlewares) - 1; i >= 0; i-- {
		mw := middlewares[i] // 捕获当前迭代的中间件
		next := executor     // 捕获当前的下一个执行器
		executor = func(ctx httpx.IContext) error {
			return mw(ctx, func() error {
				return next(ctx)
			})
		}
	}

	// 预获取错误处理器
	errorHandler := DefaultErrorHandler
	if rb.config != nil && rb.config.Response.ErrorHandler != nil {
		errorHandler = rb.config.Response.ErrorHandler
	}

	// 预获取 MaxBodySize
	maxBodySize := int64(0)
	if rb.config != nil {
		maxBodySize = rb.config.Body.MaxBodySize
	}

	return func(c httpx.IContext) error {
		if maxBodySize > 0 {
			c.Set(httpx.MaxBodySizeKey, httpx.ValueOf(maxBodySize))
		}

		if err := executor(c); err != nil {
			response := errorHandler(c, err)
			if response == nil {
				response = DefaultErrorHandler(c, err)
			}
			return response.Send(c)
		}

		return nil
	}
}
