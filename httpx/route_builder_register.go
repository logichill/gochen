package httpx

import (
	"strings"

	"gochen/errors"
)

// Build 构建并注册所有路由。
func (b *RouteBuilder) Build() error {
	if b == nil {
		return errors.NewCode(errors.InvalidInput, "route builder cannot be nil")
	}
	if isNilLike(b.group) {
		return errors.NewCode(errors.InvalidInput, "route group cannot be nil")
	}
	if len(b.errors) > 0 {
		return errors.NewCode(errors.InvalidInput, "route builder has errors").
			WithContext("errors", b.errors)
	}

	if isNilLike(b.permissionChecker) {
		for _, route := range b.routes {
			if len(route.Permissions) > 0 || len(route.Roles) > 0 {
				return errors.NewCode(errors.InvalidInput, "route requires permission checker").
					WithContext("path", route.Path).
					WithContext("method", route.Method)
			}
		}
	}

	for _, route := range b.routes {
		handler := b.wrapHandler(route)
		if err := b.registerRoute(route.Method, route.Path, handler); err != nil {
			return err
		}
	}

	return nil
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
