package httpx

import (
	"gochen/errors"
)

func (b *RouteBuilder) executeMiddlewareChain(ctx IContext, middlewares []Middleware, handler Handler) error {
	// 已被上游中间件 Abort 的请求必须阻断：与 nethttp.Server.executeMiddlewareChain 对齐，
	// 否则认证/鉴权/限流中间件显式终止后，后续中间件与业务 Handler 仍会执行，
	// 造成越权副作用或重复响应写入。
	if ctx.IsAborted() {
		return nil
	}
	if len(middlewares) == 0 {
		return handler(ctx)
	}

	// 链的中止语义由中间件自身与 ctx（IsAborted）承担：中间件可以不调用 next
	// 并返回 nil 来终止链；本函数只负责顺序调用与错误透传，不额外区分中止。
	return middlewares[0](ctx, func() error {
		return b.executeMiddlewareChain(ctx, middlewares[1:], handler)
	})
}

// wrapHandler 包装处理器，注入权限检查和中间件。
//
// 中间件链在 Build 阶段（本函数调用时）一次性组装完毕，返回的 Handler
// 只负责执行已固化的链：避免每个请求都重新分配切片与权限检查闭包，
// 降低高 QPS 下的 GC 压力。
func (b *RouteBuilder) wrapHandler(route RouteConfig) Handler {
	middlewares := make([]Middleware, 0, len(b.globalMiddlewares)+len(route.Middlewares)+1)
	middlewares = append(middlewares, b.globalMiddlewares...)

	// 权限检查中间件（在全局中间件后、路由级中间件前执行）。
	if !isNilLike(b.permissionChecker) && (len(route.Permissions) > 0 || len(route.Roles) > 0) {
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
	handler := route.Handler

	return func(ctx IContext) error {
		if ctx == nil {
			return errors.NewCode(errors.InvalidInput, "ctx is nil")
		}
		return b.executeMiddlewareChain(ctx, middlewares, handler)
	}
}
