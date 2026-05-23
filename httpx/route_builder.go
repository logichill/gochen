package httpx

import (
	"strings"

	"gochen/errors"
)

// RouteConfig 声明式路由配置。
//
// 说明：
// - 支持在路由定义时声明权限和角色要求；
// - 通过 RouteBuilder 批量注册路由。
type RouteConfig struct {
	// Path 路由路径
	Path string
	// Method HTTP 方法（GET/POST/PUT/DELETE/PATCH/HEAD/OPTIONS）
	Method string
	// Handler 处理函数
	Handler Handler
	// Permissions 所需权限列表（满足任一即可，空表示无权限要求）
	Permissions []string
	// Roles 所需角色列表（满足任一即可，空表示无角色要求）
	Roles []string
	// Middlewares 路由级中间件（在权限检查后执行）
	Middlewares []Middleware
	// Description 路由描述（用于文档生成）
	Description string
	// Tags 路由标签（用于分组和文档生成）
	Tags []string
}

// IPermissionChecker 权限检查器接口。
//
// 说明：
// - 由业务层实现，用于检查用户是否具有指定权限或角色；
// - RouteBuilder 在注册路由时自动注入权限检查中间件。
type IPermissionChecker interface {
	// HasPermission 检查当前用户是否具有指定权限
	HasPermission(ctx IContext, permission string) bool
	// HasRole 检查当前用户是否具有指定角色
	HasRole(ctx IContext, role string) bool
	// HasAnyPermission 检查当前用户是否具有任一权限
	HasAnyPermission(ctx IContext, permissions []string) bool
	// HasAnyRole 检查当前用户是否具有任一角色
	HasAnyRole(ctx IContext, roles []string) bool
}

// RouteBuilder 声明式路由构建器。
//
// 说明：
// - 提供流畅的 API 批量注册路由；
// - 自动注入权限检查中间件。
//
// 用法示例：
//
//	builder := httpx.NewRouteBuilder(group).
//	    WithPermissionChecker(checker).
//	    Route(httpx.RouteConfig{
//	        Path:        "/users",
//	        Method:      "GET",
//	        Handler:     listUsers,
//	        Permissions: []string{"user:read"},
//	    }).
//	    Route(httpx.RouteConfig{
//	        Path:        "/users",
//	        Method:      "POST",
//	        Handler:     createUser,
//	        Permissions: []string{"user:write"},
//	        Roles:       []string{"admin"},
//	    })
//
//	if err := builder.Build(); err != nil {
//	    return err
//	}
type RouteBuilder struct {
	group             IRouteGroup
	permissionChecker IPermissionChecker
	routes            []RouteConfig
	globalMiddlewares []Middleware
	errors            []error
}

// isAllowedHTTPMethod 判断AllowedHTTPMethod。
func isAllowedHTTPMethod(method string) bool {
	switch method {
	case "GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS":
		return true
	default:
		return false
	}
}

// NewRouteBuilder 创建路由构建器。
func NewRouteBuilder(group IRouteGroup) *RouteBuilder {
	return &RouteBuilder{
		group:  group,
		routes: make([]RouteConfig, 0),
		errors: make([]error, 0),
	}
}

// WithPermissionChecker 设置权限检查器。
func (b *RouteBuilder) WithPermissionChecker(checker IPermissionChecker) *RouteBuilder {
	b.permissionChecker = checker
	return b
}

// WithMiddlewares 设置全局中间件（应用于所有路由）。
func (b *RouteBuilder) WithMiddlewares(middlewares ...Middleware) *RouteBuilder {
	b.globalMiddlewares = append(b.globalMiddlewares, middlewares...)
	return b
}

// Route 添加路由配置。
func (b *RouteBuilder) Route(cfg RouteConfig) *RouteBuilder {
	if cfg.Path == "" {
		b.errors = append(b.errors, errors.NewCode(errors.InvalidInput, "route path cannot be empty"))
		return b
	}
	if cfg.Method == "" {
		b.errors = append(b.errors, errors.NewCode(errors.InvalidInput, "route method cannot be empty").
			WithContext("path", cfg.Path))
		return b
	}
	cfg.Method = strings.ToUpper(strings.TrimSpace(cfg.Method))
	if !isAllowedHTTPMethod(cfg.Method) {
		b.errors = append(b.errors, errors.NewCode(errors.InvalidInput, "unknown http method").
			WithContext("path", cfg.Path).
			WithContext("method", cfg.Method))
		return b
	}
	if cfg.Handler == nil {
		b.errors = append(b.errors, errors.NewCode(errors.InvalidInput, "route handler cannot be nil").
			WithContext("path", cfg.Path).
			WithContext("method", cfg.Method))
		return b
	}
	b.routes = append(b.routes, cfg)
	return b
}

// GET 添加 GET 路由（简化方法）。
func (b *RouteBuilder) GET(path string, handler Handler, permissions ...string) *RouteBuilder {
	return b.Route(RouteConfig{
		Path:        path,
		Method:      "GET",
		Handler:     handler,
		Permissions: permissions,
	})
}

// POST 添加 POST 路由（简化方法）。
func (b *RouteBuilder) POST(path string, handler Handler, permissions ...string) *RouteBuilder {
	return b.Route(RouteConfig{
		Path:        path,
		Method:      "POST",
		Handler:     handler,
		Permissions: permissions,
	})
}

// PUT 添加 PUT 路由（简化方法）。
func (b *RouteBuilder) PUT(path string, handler Handler, permissions ...string) *RouteBuilder {
	return b.Route(RouteConfig{
		Path:        path,
		Method:      "PUT",
		Handler:     handler,
		Permissions: permissions,
	})
}

// DELETE 添加 DELETE 路由（简化方法）。
func (b *RouteBuilder) DELETE(path string, handler Handler, permissions ...string) *RouteBuilder {
	return b.Route(RouteConfig{
		Path:        path,
		Method:      "DELETE",
		Handler:     handler,
		Permissions: permissions,
	})
}

// PATCH 添加 PATCH 路由（简化方法）。
func (b *RouteBuilder) PATCH(path string, handler Handler, permissions ...string) *RouteBuilder {
	return b.Route(RouteConfig{
		Path:        path,
		Method:      "PATCH",
		Handler:     handler,
		Permissions: permissions,
	})
}

// Build 构建并注册所有路由。
func (b *RouteBuilder) Build() error {
	if len(b.errors) > 0 {
		return errors.NewCode(errors.InvalidInput, "route builder has errors").
			WithContext("errors", b.errors)
	}

	for _, route := range b.routes {
		handler := b.wrapHandler(route)
		if err := b.registerRoute(route.Method, route.Path, handler); err != nil {
			return err
		}
	}

	return nil
}

// Routes 返回已配置的路由列表（用于文档生成）。
func (b *RouteBuilder) Routes() []RouteConfig {
	return b.routes
}

func (b *RouteBuilder) executeMiddlewareChain(ctx IContext, middlewares []Middleware, handler Handler) error {
	if len(middlewares) == 0 {
		return handler(ctx)
	}

	var nextCalled bool
	err := middlewares[0](ctx, func() error {
		nextCalled = true
		return b.executeMiddlewareChain(ctx, middlewares[1:], handler)
	})
	if err != nil {
		return err
	}

	if ctx.IsAborted() || !nextCalled {
		return nil
	}
	return nil
}

// wrapHandler 包装处理器，注入权限检查和中间件。
func (b *RouteBuilder) wrapHandler(route RouteConfig) Handler {
	return func(ctx IContext) error {
		if ctx == nil {
			return errors.NewCode(errors.InvalidInput, "ctx is nil")
		}

		middlewares := make([]Middleware, 0, len(b.globalMiddlewares)+len(route.Middlewares)+1)
		middlewares = append(middlewares, b.globalMiddlewares...)

		// 权限检查中间件（在全局中间件后、路由级中间件前执行）。
		if b.permissionChecker != nil && (len(route.Permissions) > 0 || len(route.Roles) > 0) {
			checker := b.permissionChecker
			perms := append([]string(nil), route.Permissions...)
			roles := append([]string(nil), route.Roles...)
			middlewares = append(middlewares, func(ctx IContext, next func() error) error {
				if len(perms) > 0 && !checker.HasAnyPermission(ctx, perms) {
					return errors.NewCode(errors.Forbidden, "permission denied").
						WithContext("required_permissions", perms)
				}
				if len(roles) > 0 && !checker.HasAnyRole(ctx, roles) {
					return errors.NewCode(errors.Forbidden, "role required").
						WithContext("required_roles", roles)
				}
				return next()
			})
		}

		middlewares = append(middlewares, route.Middlewares...)
		return b.executeMiddlewareChain(ctx, middlewares, route.Handler)
	}
}

// registerRoute 注册单个路由。
func (b *RouteBuilder) registerRoute(method, path string, handler Handler) error {
	switch strings.ToUpper(method) {
	case "GET":
		b.group.GET(path, handler)
	case "POST":
		b.group.POST(path, handler)
	case "PUT":
		b.group.PUT(path, handler)
	case "DELETE":
		b.group.DELETE(path, handler)
	case "PATCH":
		b.group.PATCH(path, handler)
	case "HEAD":
		b.group.HEAD(path, handler)
	case "OPTIONS":
		b.group.OPTIONS(path, handler)
	default:
		return errors.NewCode(errors.InvalidInput, "unknown http method").
			WithContext("path", path).
			WithContext("method", method)
	}
	return nil
}

// PermissionMiddleware 创建权限检查中间件。
//
// 说明：
// - 用于需要单独使用权限检查的场景；
// - RouteBuilder 内部已集成权限检查。
func PermissionMiddleware(checker IPermissionChecker, permissions ...string) Middleware {
	if checker == nil {
		return func(IContext, func() error) error {
			return errors.NewCode(errors.InvalidInput, "permission checker is nil")
		}
	}
	return func(ctx IContext, next func() error) error {
		if len(permissions) > 0 && !checker.HasAnyPermission(ctx, permissions) {
			return errors.NewCode(errors.Forbidden, "permission denied").
				WithContext("required_permissions", permissions)
		}
		return next()
	}
}

// RoleMiddleware 创建角色检查中间件。
func RoleMiddleware(checker IPermissionChecker, roles ...string) Middleware {
	if checker == nil {
		return func(IContext, func() error) error {
			return errors.NewCode(errors.InvalidInput, "role checker is nil")
		}
	}
	return func(ctx IContext, next func() error) error {
		if len(roles) > 0 && !checker.HasAnyRole(ctx, roles) {
			return errors.NewCode(errors.Forbidden, "role required").
				WithContext("required_roles", roles)
		}
		return next()
	}
}
