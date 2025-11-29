package api

import (
	"fmt"
	"strconv"

	app "gochen/app"
	"gochen/domain/entity"
	sservice "gochen/domain/service"
	"gochen/errors"
	core "gochen/http"
)

// AuditedRouteBuilder 面向审计型实体的 REST 路由构建器
// 约定：T 必须实现 entity.Entity[int64]（含审计/软删能力），服务实现 IAuditedService。
type AuditedRouteBuilder[T entity.Entity[int64]] struct {
	config      *RouteConfig
	middlewares []core.Middleware
	service     sservice.IAuditedService[T, int64]
	app         app.IApplication[T] // 可选，用于高级查询/分页/批量
}

// NewAuditedRouteBuilder 创建审计型路由构建器
func NewAuditedRouteBuilder[T entity.Entity[int64]](svc sservice.IAuditedService[T, int64]) *AuditedRouteBuilder[T] {
	b := &AuditedRouteBuilder[T]{config: DefaultRouteConfig(), service: svc}
	// 若服务同时实现了 IAppService，则可启用高级查询/分页/批量
	if a, ok := any(svc).(app.IApplication[T]); ok {
		b.app = a
	}
	return b
}

// WithConfig 配置路由行为
func (rb *AuditedRouteBuilder[T]) WithConfig(cfg *RouteConfig) *AuditedRouteBuilder[T] {
	if cfg != nil {
		rb.config = cfg
	}
	return rb
}

// Use 注册中间件
func (rb *AuditedRouteBuilder[T]) Use(middlewares ...core.Middleware) *AuditedRouteBuilder[T] {
	rb.middlewares = append(rb.middlewares, middlewares...)
	return rb
}

// Register 注册到路由组
func (rb *AuditedRouteBuilder[T]) Register(group core.IRouteGroup) error {
	if rb.service == nil {
		return fmt.Errorf("service cannot be nil")
	}
	rb.registerListRoutes(group)
	rb.registerEntityRoutes(group)
	if rb.config.EnableBatch && rb.app != nil {
		rb.registerBatchRoutes(group)
	}
	// 审计扩展
	rb.registerAuditRoutes(group)
	return nil
}

func (rb *AuditedRouteBuilder[T]) registerListRoutes(group core.IRouteGroup) {
	base := rb.base()
	group.GET(base, rb.wrap(rb.handleList))
	group.GET(fmt.Sprintf("%s/deleted", base), rb.wrap(rb.handleListDeleted))
}

func (rb *AuditedRouteBuilder[T]) registerEntityRoutes(group core.IRouteGroup) {
	base := rb.base()
	group.GET(fmt.Sprintf("%s/:id", base), rb.wrap(rb.handleGet))
	group.POST(base, rb.wrap(rb.handleCreate))
	group.PUT(fmt.Sprintf("%s/:id", base), rb.wrap(rb.handleUpdate))
	// DELETE：支持软删/硬删
	group.DELETE(fmt.Sprintf("%s/:id", base), rb.wrap(rb.handleDelete))
}

func (rb *AuditedRouteBuilder[T]) registerBatchRoutes(group core.IRouteGroup) {
	base := rb.base()
	group.POST(fmt.Sprintf("%s/batch", base), rb.wrap(rb.handleCreateBatch))
	group.PUT(fmt.Sprintf("%s/batch", base), rb.wrap(rb.handleUpdateBatch))
	group.DELETE(fmt.Sprintf("%s/batch", base), rb.wrap(rb.handleDeleteBatch))
}

func (rb *AuditedRouteBuilder[T]) registerAuditRoutes(group core.IRouteGroup) {
	base := rb.base()
	// 审计追踪
	group.GET(fmt.Sprintf("%s/:id/audit", base), rb.wrap(rb.handleAuditTrail))
	// 恢复
	group.POST(fmt.Sprintf("%s/:id/restore", base), rb.wrap(rb.handleRestore))
}

func (rb *AuditedRouteBuilder[T]) base() string {
	if rb.config != nil && rb.config.BasePath != "" {
		return rb.config.BasePath
	}
	return ""
}

func (rb *AuditedRouteBuilder[T]) wrap(handler func(core.IHttpContext) error) func(core.IHttpContext) error {
	return func(c core.IHttpContext) error {
		mws := append([]core.Middleware{}, rb.middlewares...)
		if rb.config != nil && len(rb.config.Middlewares) > 0 {
			mws = append(mws, rb.config.Middlewares...)
		}
		exec := handler
		for i := len(mws) - 1; i >= 0; i-- {
			mw := mws[i]
			next := exec
			exec = func(ctx core.IHttpContext) error { return mw(ctx, func() error { return next(ctx) }) }
		}
		if err := exec(c); err != nil {
			eh := DefaultErrorHandler
			if rb.config != nil && rb.config.ErrorHandler != nil {
				eh = rb.config.ErrorHandler
			}
			return eh(err).Send(c)
		}
		return nil
	}
}

// Handlers
func (rb *AuditedRouteBuilder[T]) handleList(c core.IHttpContext) error {
	if rb.config.EnablePagination {
		return rb.handlePagedList(c)
	}
	if rb.app != nil {
		query := rb.parseQueryParams(c)
		result, err := rb.app.ListByQuery(c.GetContext(), query)
		if err != nil {
			return err
		}
		return c.JSON(200, rb.config.ResponseWrapper(result))
	}
	// 退化为简单列表
	data, err := rb.service.List(c.GetContext(), 0, 1000)
	if err != nil {
		return err
	}
	return c.JSON(200, rb.config.ResponseWrapper(data))
}

func (rb *AuditedRouteBuilder[T]) handlePagedList(c core.IHttpContext) error {
	if rb.app != nil {
		opts := rb.parsePaginationOptions(c)
		result, err := rb.app.ListPage(c.GetContext(), opts)
		if err != nil {
			return err
		}
		return c.JSON(200, rb.config.ResponseWrapper(result))
	}
	// 退化为手动分页
	opts := rb.parsePaginationOptions(c)
	offset := (opts.Page - 1) * opts.Size
	data, err := rb.service.List(c.GetContext(), offset, opts.Size)
	if err != nil {
		return err
	}
	total, err := rb.service.Count(c.GetContext())
	if err != nil {
		return err
	}
	pr := &app.PagedResult[T]{Data: data, Total: total, Page: opts.Page, Size: opts.Size}
	if opts.Size > 0 {
		pr.TotalPages = int((total + int64(opts.Size) - 1) / int64(opts.Size))
	}
	pr.HasNext = pr.Page < pr.TotalPages
	pr.HasPrev = pr.Page > 1
	return c.JSON(200, rb.config.ResponseWrapper(pr))
}

func (rb *AuditedRouteBuilder[T]) handleGet(c core.IHttpContext) error {
	id, err := strconv.ParseInt(c.GetParam("id"), 10, 64)
	if err != nil {
		return errors.NewValidationError("无效的ID格式")
	}
	e, err := rb.service.GetByID(c.GetContext(), id)
	if err != nil {
		return err
	}
	return c.JSON(200, rb.config.ResponseWrapper(e))
}

func (rb *AuditedRouteBuilder[T]) handleCreate(c core.IHttpContext) error {
	var e T
	if err := c.BindJSON(&e); err != nil {
		return errors.NewValidationError("无效的请求数据")
	}
	if rb.config.Validator != nil {
		if err := rb.config.Validator(e); err != nil {
			return err
		}
	}
	if err := rb.service.Create(c.GetContext(), e); err != nil {
		return err
	}
	return c.JSON(200, rb.config.ResponseWrapper(map[string]any{"id": e.GetID()}))
}

func (rb *AuditedRouteBuilder[T]) handleUpdate(c core.IHttpContext) error {
	id, err := strconv.ParseInt(c.GetParam("id"), 10, 64)
	if err != nil {
		return errors.NewValidationError("无效的ID格式")
	}
	e, err := rb.service.GetByID(c.GetContext(), id)
	if err != nil {
		return err
	}
	if err := c.BindJSON(&e); err != nil {
		return errors.NewValidationError("无效的请求数据")
	}
	if rb.config.Validator != nil {
		if err := rb.config.Validator(e); err != nil {
			return err
		}
	}
	if err := rb.service.Update(c.GetContext(), e); err != nil {
		return err
	}
	return c.JSON(200, rb.config.ResponseWrapper(e))
}

func (rb *AuditedRouteBuilder[T]) handleDelete(c core.IHttpContext) error {
	id, err := strconv.ParseInt(c.GetParam("id"), 10, 64)
	if err != nil {
		return errors.NewValidationError("无效的ID格式")
	}
	// 支持硬删：?hard=true
	hard := c.GetQuery("hard") == "true"
	if hard {
		if err := rb.service.PermanentDelete(c.GetContext(), id); err != nil {
			return err
		}
	} else {
		// 操作人从请求上下文扩展，缺省为 system
		by := c.GetHeader("X-Operator")
		if by == "" {
			by = "system"
		}
		if err := rb.service.SoftDelete(c.GetContext(), id, by); err != nil {
			return err
		}
	}
	return c.JSON(200, rb.config.ResponseWrapper(nil))
}

func (rb *AuditedRouteBuilder[T]) handleCreateBatch(c core.IHttpContext) error {
	if rb.app == nil {
		return errors.NewValidationError("未启用批量操作")
	}
	var es []T
	if err := c.BindJSON(&es); err != nil {
		return errors.NewValidationError("无效的请求数据")
	}
	res, err := rb.app.CreateBatch(c.GetContext(), es)
	if err != nil {
		return err
	}
	return c.JSON(201, rb.config.ResponseWrapper(res))
}
func (rb *AuditedRouteBuilder[T]) handleUpdateBatch(c core.IHttpContext) error {
	if rb.app == nil {
		return errors.NewValidationError("未启用批量操作")
	}
	var es []T
	if err := c.BindJSON(&es); err != nil {
		return errors.NewValidationError("无效的请求数据")
	}
	res, err := rb.app.UpdateBatch(c.GetContext(), es)
	if err != nil {
		return err
	}
	return c.JSON(200, rb.config.ResponseWrapper(res))
}
func (rb *AuditedRouteBuilder[T]) handleDeleteBatch(c core.IHttpContext) error {
	if rb.app == nil {
		return errors.NewValidationError("未启用批量操作")
	}
	var req struct {
		IDs []int64 `json:"ids"`
	}
	if err := c.BindJSON(&req); err != nil {
		return errors.NewValidationError("无效的请求数据")
	}
	res, err := rb.app.DeleteBatch(c.GetContext(), req.IDs)
	if err != nil {
		return err
	}
	return c.JSON(200, rb.config.ResponseWrapper(res))
}

func (rb *AuditedRouteBuilder[T]) handleListDeleted(c core.IHttpContext) error {
	// 分页参数可选
	opts := rb.parsePaginationOptions(c)
	offset := (opts.Page - 1) * opts.Size
	data, err := rb.service.ListDeleted(c.GetContext(), offset, opts.Size)
	if err != nil {
		return err
	}
	return c.JSON(200, rb.config.ResponseWrapper(data))
}

func (rb *AuditedRouteBuilder[T]) handleAuditTrail(c core.IHttpContext) error {
	id, err := strconv.ParseInt(c.GetParam("id"), 10, 64)
	if err != nil {
		return errors.NewValidationError("无效的ID格式")
	}
	recs, err := rb.service.GetAuditTrail(c.GetContext(), id)
	if err != nil {
		return err
	}
	return c.JSON(200, rb.config.ResponseWrapper(recs))
}

func (rb *AuditedRouteBuilder[T]) handleRestore(c core.IHttpContext) error {
	id, err := strconv.ParseInt(c.GetParam("id"), 10, 64)
	if err != nil {
		return errors.NewValidationError("无效的ID格式")
	}
	var req struct {
		By string `json:"by"`
	}
	_ = c.BindJSON(&req)
	if req.By == "" {
		req.By = c.GetHeader("X-Operator")
	}
	if req.By == "" {
		req.By = "system"
	}
	if err := rb.service.Restore(c.GetContext(), id, req.By); err != nil {
		return err
	}
	return c.JSON(200, rb.config.ResponseWrapper(nil))
}

// 共用解析辅助
func (rb *AuditedRouteBuilder[T]) parseQueryParams(c core.IHttpContext) *app.QueryParams {
	return (&RouteBuilder[T]{}).parseQueryParams(c)
}
func (rb *AuditedRouteBuilder[T]) parsePaginationOptions(c core.IHttpContext) *app.PaginationOptions {
	// 使用普通 RouteBuilder 的实现以保持一致
	return (&RouteBuilder[T]{config: rb.config}).parsePaginationOptions(c)
}
