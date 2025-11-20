// Package mocks 提供示例中使用的模拟组件
package mocks

import (
	"fmt"
	"log"

	httpx "gochen/httpx"
)

// Route 路由信息
type Route struct {
	Method  string
	Path    string
	Handler httpx.HttpHandler
}

// MockRouter 模拟路由器
type MockRouter struct {
	Routes []Route
}

// NewMockRouter 创建模拟路由器
func NewMockRouter() *MockRouter {
	return &MockRouter{
		Routes: make([]Route, 0),
	}
}

func (r *MockRouter) GET(path string, handler httpx.HttpHandler) httpx.IRouteGroup {
	r.Routes = append(r.Routes, Route{Method: "GET", Path: path, Handler: handler})
	return r
}

func (r *MockRouter) POST(path string, handler httpx.HttpHandler) httpx.IRouteGroup {
	r.Routes = append(r.Routes, Route{Method: "POST", Path: path, Handler: handler})
	return r
}

func (r *MockRouter) PUT(path string, handler httpx.HttpHandler) httpx.IRouteGroup {
	r.Routes = append(r.Routes, Route{Method: "PUT", Path: path, Handler: handler})
	return r
}

func (r *MockRouter) DELETE(path string, handler httpx.HttpHandler) httpx.IRouteGroup {
	r.Routes = append(r.Routes, Route{Method: "DELETE", Path: path, Handler: handler})
	return r
}

func (r *MockRouter) PATCH(path string, handler httpx.HttpHandler) httpx.IRouteGroup {
	r.Routes = append(r.Routes, Route{Method: "PATCH", Path: path, Handler: handler})
	return r
}

func (r *MockRouter) HEAD(path string, handler httpx.HttpHandler) httpx.IRouteGroup {
	r.Routes = append(r.Routes, Route{Method: "HEAD", Path: path, Handler: handler})
	return r
}

func (r *MockRouter) OPTIONS(path string, handler httpx.HttpHandler) httpx.IRouteGroup {
	r.Routes = append(r.Routes, Route{Method: "OPTIONS", Path: path, Handler: handler})
	return r
}

func (r *MockRouter) Group(prefix string) httpx.IRouteGroup {
	// 简化实现，返回自身
	return r
}

func (r *MockRouter) Use(middlewares ...httpx.Middleware) httpx.IRouteGroup {
	// 简化实现，不做实际处理
	return r
}

func (r *MockRouter) RegisterRoute(method, path string, handler any) {
	// 简化实现
}

func (r *MockRouter) RegisterMiddleware(middleware any) {
	// 简化实现
}

// PrintRoutes 打印路由信息
func (r *MockRouter) PrintRoutes() {
	log.Printf("注册的路由:")
	for _, route := range r.Routes {
		log.Printf("  %s %s", route.Method, route.Path)
	}
}

// AdvancedRoute 高级路由信息（包含中间件）
type AdvancedRoute struct {
	Method      string
	Path        string
	Middlewares []string
	Handler     httpx.HttpHandler
}

// MockAdvancedRouter 模拟高级路由器（支持中间件）
type MockAdvancedRouter struct {
	Routes []AdvancedRoute
}

// NewMockAdvancedRouter 创建模拟高级路由器
func NewMockAdvancedRouter() *MockAdvancedRouter {
	return &MockAdvancedRouter{
		Routes: make([]AdvancedRoute, 0),
	}
}

func (r *MockAdvancedRouter) GET(path string, handler httpx.HttpHandler) httpx.IRouteGroup {
	r.Routes = append(r.Routes, AdvancedRoute{
		Method:      "GET",
		Path:        path,
		Middlewares: []string{"logging", "auth", "rate-limit"},
		Handler:     handler,
	})
	return r
}

func (r *MockAdvancedRouter) POST(path string, handler httpx.HttpHandler) httpx.IRouteGroup {
	r.Routes = append(r.Routes, AdvancedRoute{
		Method:      "POST",
		Path:        path,
		Middlewares: []string{"logging", "auth", "rate-limit"},
		Handler:     handler,
	})
	return r
}

func (r *MockAdvancedRouter) PUT(path string, handler httpx.HttpHandler) httpx.IRouteGroup {
	r.Routes = append(r.Routes, AdvancedRoute{
		Method:      "PUT",
		Path:        path,
		Middlewares: []string{"logging", "auth", "rate-limit"},
		Handler:     handler,
	})
	return r
}

func (r *MockAdvancedRouter) DELETE(path string, handler httpx.HttpHandler) httpx.IRouteGroup {
	r.Routes = append(r.Routes, AdvancedRoute{
		Method:      "DELETE",
		Path:        path,
		Middlewares: []string{"logging", "auth", "rate-limit"},
		Handler:     handler,
	})
	return r
}

func (r *MockAdvancedRouter) PATCH(path string, handler httpx.HttpHandler) httpx.IRouteGroup {
	r.Routes = append(r.Routes, AdvancedRoute{
		Method:      "PATCH",
		Path:        path,
		Middlewares: []string{"logging", "auth", "rate-limit"},
		Handler:     handler,
	})
	return r
}

func (r *MockAdvancedRouter) HEAD(path string, handler httpx.HttpHandler) httpx.IRouteGroup {
	r.Routes = append(r.Routes, AdvancedRoute{
		Method:      "HEAD",
		Path:        path,
		Middlewares: []string{"logging", "auth", "rate-limit"},
		Handler:     handler,
	})
	return r
}

func (r *MockAdvancedRouter) OPTIONS(path string, handler httpx.HttpHandler) httpx.IRouteGroup {
	r.Routes = append(r.Routes, AdvancedRoute{
		Method:      "OPTIONS",
		Path:        path,
		Middlewares: []string{"logging", "auth", "rate-limit"},
		Handler:     handler,
	})
	return r
}

func (r *MockAdvancedRouter) Group(prefix string) httpx.IRouteGroup {
	return r
}

func (r *MockAdvancedRouter) Use(middlewares ...httpx.Middleware) httpx.IRouteGroup {
	return r
}

func (r *MockAdvancedRouter) RegisterRoute(method, path string, handler any) {
}

func (r *MockAdvancedRouter) RegisterMiddleware(middleware any) {
}

// PrintRoutes 打印路由信息
func (r *MockAdvancedRouter) PrintRoutes() {
	log.Printf("注册的高级路由:")
	for _, route := range r.Routes {
		middlewareStr := ""
		if len(route.Middlewares) > 0 {
			middlewareStr = fmt.Sprintf(" [%s]", route.Middlewares[0])
			for i := 1; i < len(route.Middlewares); i++ {
				middlewareStr += fmt.Sprintf(" -> %s", route.Middlewares[i])
			}
		}
		log.Printf("  %s %s%s", route.Method, route.Path, middlewareStr)
	}
}

// MiddlewareRoute 带中间件的路由信息
type MiddlewareRoute struct {
	Method      string
	Path        string
	Middlewares []string
	Handler     httpx.HttpHandler
}

// MockRouterWithMiddleware 模拟支持中间件的路由器
type MockRouterWithMiddleware struct {
	Routes []MiddlewareRoute
}

// NewMockRouterWithMiddleware 创建带中间件的模拟路由器
func NewMockRouterWithMiddleware() *MockRouterWithMiddleware {
	return &MockRouterWithMiddleware{
		Routes: make([]MiddlewareRoute, 0),
	}
}

func (r *MockRouterWithMiddleware) GET(path string, handler httpx.HttpHandler) httpx.IRouteGroup {
	r.Routes = append(r.Routes, MiddlewareRoute{
		Method:  "GET",
		Path:    path,
		Handler: handler,
	})
	return r
}

func (r *MockRouterWithMiddleware) POST(path string, handler httpx.HttpHandler) httpx.IRouteGroup {
	r.Routes = append(r.Routes, MiddlewareRoute{
		Method:  "POST",
		Path:    path,
		Handler: handler,
	})
	return r
}

func (r *MockRouterWithMiddleware) PUT(path string, handler httpx.HttpHandler) httpx.IRouteGroup {
	r.Routes = append(r.Routes, MiddlewareRoute{
		Method:  "PUT",
		Path:    path,
		Handler: handler,
	})
	return r
}

func (r *MockRouterWithMiddleware) DELETE(path string, handler httpx.HttpHandler) httpx.IRouteGroup {
	r.Routes = append(r.Routes, MiddlewareRoute{
		Method:  "DELETE",
		Path:    path,
		Handler: handler,
	})
	return r
}

func (r *MockRouterWithMiddleware) PATCH(path string, handler httpx.HttpHandler) httpx.IRouteGroup {
	r.Routes = append(r.Routes, MiddlewareRoute{
		Method:  "PATCH",
		Path:    path,
		Handler: handler,
	})
	return r
}

func (r *MockRouterWithMiddleware) HEAD(path string, handler httpx.HttpHandler) httpx.IRouteGroup {
	r.Routes = append(r.Routes, MiddlewareRoute{
		Method:  "HEAD",
		Path:    path,
		Handler: handler,
	})
	return r
}

func (r *MockRouterWithMiddleware) OPTIONS(path string, handler httpx.HttpHandler) httpx.IRouteGroup {
	r.Routes = append(r.Routes, MiddlewareRoute{
		Method:  "OPTIONS",
		Path:    path,
		Handler: handler,
	})
	return r
}

func (r *MockRouterWithMiddleware) Group(prefix string) httpx.IRouteGroup {
	return r
}

func (r *MockRouterWithMiddleware) Use(middlewares ...httpx.Middleware) httpx.IRouteGroup {
	return r
}

func (r *MockRouterWithMiddleware) RegisterRoute(method, path string, handler any) {
}

func (r *MockRouterWithMiddleware) RegisterMiddleware(middleware any) {
}

// PrintRoutes 打印路由信息
func (r *MockRouterWithMiddleware) PrintRoutes() {
	log.Printf("注册的路由及中间件:")
	for _, route := range r.Routes {
		middlewareStr := ""
		if len(route.Middlewares) > 0 {
			middlewareStr = fmt.Sprintf(" [%s]", route.Middlewares[0])
			for i := 1; i < len(route.Middlewares); i++ {
				middlewareStr += fmt.Sprintf(" -> %s", route.Middlewares[i])
			}
		}
		log.Printf("  %s %s%s", route.Method, route.Path, middlewareStr)
	}
}
