// Package mocks 提供示例中使用的模拟组件
package mocks

import (
	"context"
	"fmt"
	"log"
	"strings"

	"gochen/httpx"
)

// Route 表示一条简单的路由注册记录。
type Route struct {
	Method  string
	Path    string
	Handler httpx.Handler
}

// MockRouter 是只记录路由信息的最小路由器桩实现。
type MockRouter struct {
	Routes      []Route
	prefix      string
	middlewares []httpx.Middleware
	routeSink   *[]Route
}

// NewMockRouter 创建一个空的 mock 路由器。
func NewMockRouter() *MockRouter {
	return &MockRouter{
		Routes: make([]Route, 0),
	}
}

// GET 记录一条 GET 路由。
func (r *MockRouter) GET(path string, handler httpx.Handler) httpx.IRouteGroup {
	r.addRoute("GET", path, handler)
	return r
}

// POST 记录一条 POST 路由。
func (r *MockRouter) POST(path string, handler httpx.Handler) httpx.IRouteGroup {
	r.addRoute("POST", path, handler)
	return r
}

// PUT 记录一条 PUT 路由。
func (r *MockRouter) PUT(path string, handler httpx.Handler) httpx.IRouteGroup {
	r.addRoute("PUT", path, handler)
	return r
}

// DELETE 记录一条 DELETE 路由。
func (r *MockRouter) DELETE(path string, handler httpx.Handler) httpx.IRouteGroup {
	r.addRoute("DELETE", path, handler)
	return r
}

// PATCH 记录一条 PATCH 路由。
func (r *MockRouter) PATCH(path string, handler httpx.Handler) httpx.IRouteGroup {
	r.addRoute("PATCH", path, handler)
	return r
}

// HEAD 记录一条 HEAD 路由。
func (r *MockRouter) HEAD(path string, handler httpx.Handler) httpx.IRouteGroup {
	r.addRoute("HEAD", path, handler)
	return r
}

// OPTIONS 记录一条 OPTIONS 路由。
func (r *MockRouter) OPTIONS(path string, handler httpx.Handler) httpx.IRouteGroup {
	r.addRoute("OPTIONS", path, handler)
	return r
}

// Group 返回共享路由记录的子分组。
func (r *MockRouter) Group(prefix string) httpx.IRouteGroup {
	return &MockRouter{prefix: joinPath(r.prefix, prefix), middlewares: r.middlewares, routeSink: r.routes()}
}

// Use 记录中间件数量，便于示例展示 Host 的挂载顺序。
func (r *MockRouter) Use(middlewares ...httpx.Middleware) httpx.IRouteGroup {
	r.middlewares = append(r.middlewares, middlewares...)
	return r
}

// PrintRoutes 按注册顺序打印当前记录的路由。
func (r *MockRouter) PrintRoutes() {
	log.Printf("registered routes:")
	for _, route := range *r.routes() {
		log.Printf("  %s %s", route.Method, route.Path)
	}
}

func (r *MockRouter) addRoute(method, path string, handler httpx.Handler) {
	routes := r.routes()
	*routes = append(*routes, Route{Method: method, Path: r.fullPath(path), Handler: handler})
}

func (r *MockRouter) routes() *[]Route {
	if r.routeSink != nil {
		return r.routeSink
	}
	return &r.Routes
}

func (r *MockRouter) fullPath(path string) string {
	if r == nil {
		return normalizePath(path)
	}
	return joinPath(r.prefix, path)
}

// MockServer 是可注入 Host 的非阻塞 HTTP server 桩。
type MockServer struct {
	*MockRouter
	started bool
	addr    string
}

// NewMockServer 创建一个只记录路由、不监听端口的 HTTP server。
func NewMockServer() *MockServer {
	return &MockServer{MockRouter: NewMockRouter()}
}

func (s *MockServer) GET(path string, handler httpx.Handler) httpx.IServer {
	s.MockRouter.GET(path, handler)
	return s
}

func (s *MockServer) POST(path string, handler httpx.Handler) httpx.IServer {
	s.MockRouter.POST(path, handler)
	return s
}

func (s *MockServer) PUT(path string, handler httpx.Handler) httpx.IServer {
	s.MockRouter.PUT(path, handler)
	return s
}

func (s *MockServer) DELETE(path string, handler httpx.Handler) httpx.IServer {
	s.MockRouter.DELETE(path, handler)
	return s
}

func (s *MockServer) PATCH(path string, handler httpx.Handler) httpx.IServer {
	s.MockRouter.PATCH(path, handler)
	return s
}

func (s *MockServer) HEAD(path string, handler httpx.Handler) httpx.IServer {
	s.MockRouter.HEAD(path, handler)
	return s
}

func (s *MockServer) OPTIONS(path string, handler httpx.Handler) httpx.IServer {
	s.MockRouter.OPTIONS(path, handler)
	return s
}

func (s *MockServer) Use(middlewares ...httpx.Middleware) httpx.IServer {
	s.MockRouter.Use(middlewares...)
	return s
}

func (s *MockServer) Static(prefix, root string) httpx.IServer {
	s.GET(prefix, nil)
	return s
}

func (s *MockServer) ServeStatic(path, root string) {
	s.GET(path, nil)
}

func (s *MockServer) Start(addr string) error {
	s.started = true
	s.addr = addr
	return nil
}

func (s *MockServer) Stop(context.Context) error {
	s.started = false
	return nil
}

func (s *MockServer) HealthCheck() error {
	return nil
}

func normalizePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return strings.TrimSuffix(path, "/")
}

func joinPath(prefix, path string) string {
	prefix = normalizePath(prefix)
	path = normalizePath(path)
	if prefix == "" {
		if path == "" {
			return "/"
		}
		return path
	}
	if path == "" || path == "/" {
		return prefix
	}
	return prefix + path
}

// AdvancedRoute 表示带中间件链描述的路由记录。
type AdvancedRoute struct {
	Method      string
	Path        string
	Middlewares []string
	Handler     httpx.Handler
}

// MockAdvancedRouter 是带固定中间件链描述的高级路由器桩实现。
type MockAdvancedRouter struct {
	Routes []AdvancedRoute
}

// NewMockAdvancedRouter 创建一个空的高级 mock 路由器。
func NewMockAdvancedRouter() *MockAdvancedRouter {
	return &MockAdvancedRouter{
		Routes: make([]AdvancedRoute, 0),
	}
}

// GET 记录一条带默认中间件链的 GET 路由。
func (r *MockAdvancedRouter) GET(path string, handler httpx.Handler) httpx.IRouteGroup {
	r.Routes = append(r.Routes, AdvancedRoute{
		Method:      "GET",
		Path:        path,
		Middlewares: []string{"logging", "auth", "rate-limit"},
		Handler:     handler,
	})
	return r
}

// POST 记录一条带默认中间件链的 POST 路由。
func (r *MockAdvancedRouter) POST(path string, handler httpx.Handler) httpx.IRouteGroup {
	r.Routes = append(r.Routes, AdvancedRoute{
		Method:      "POST",
		Path:        path,
		Middlewares: []string{"logging", "auth", "rate-limit"},
		Handler:     handler,
	})
	return r
}

// PUT 记录一条带默认中间件链的 PUT 路由。
func (r *MockAdvancedRouter) PUT(path string, handler httpx.Handler) httpx.IRouteGroup {
	r.Routes = append(r.Routes, AdvancedRoute{
		Method:      "PUT",
		Path:        path,
		Middlewares: []string{"logging", "auth", "rate-limit"},
		Handler:     handler,
	})
	return r
}

// DELETE 记录一条带默认中间件链的 DELETE 路由。
func (r *MockAdvancedRouter) DELETE(path string, handler httpx.Handler) httpx.IRouteGroup {
	r.Routes = append(r.Routes, AdvancedRoute{
		Method:      "DELETE",
		Path:        path,
		Middlewares: []string{"logging", "auth", "rate-limit"},
		Handler:     handler,
	})
	return r
}

// PATCH 记录一条带默认中间件链的 PATCH 路由。
func (r *MockAdvancedRouter) PATCH(path string, handler httpx.Handler) httpx.IRouteGroup {
	r.Routes = append(r.Routes, AdvancedRoute{
		Method:      "PATCH",
		Path:        path,
		Middlewares: []string{"logging", "auth", "rate-limit"},
		Handler:     handler,
	})
	return r
}

// HEAD 记录一条带默认中间件链的 HEAD 路由。
func (r *MockAdvancedRouter) HEAD(path string, handler httpx.Handler) httpx.IRouteGroup {
	r.Routes = append(r.Routes, AdvancedRoute{
		Method:      "HEAD",
		Path:        path,
		Middlewares: []string{"logging", "auth", "rate-limit"},
		Handler:     handler,
	})
	return r
}

// OPTIONS 记录一条带默认中间件链的 OPTIONS 路由。
func (r *MockAdvancedRouter) OPTIONS(path string, handler httpx.Handler) httpx.IRouteGroup {
	r.Routes = append(r.Routes, AdvancedRoute{
		Method:      "OPTIONS",
		Path:        path,
		Middlewares: []string{"logging", "auth", "rate-limit"},
		Handler:     handler,
	})
	return r
}

// Group 在高级 mock 中直接复用当前实例。
func (r *MockAdvancedRouter) Group(prefix string) httpx.IRouteGroup {
	return r
}

// Use 在高级 mock 中保留为空实现。
func (r *MockAdvancedRouter) Use(middlewares ...httpx.Middleware) httpx.IRouteGroup {
	return r
}

// PrintRoutes 打印当前记录的高级路由及其中间件链。
func (r *MockAdvancedRouter) PrintRoutes() {
	log.Printf("registered advanced routes:")
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

// MiddlewareRoute 表示带中间件列表的路由记录。
type MiddlewareRoute struct {
	Method      string
	Path        string
	Middlewares []string
	Handler     httpx.Handler
}

// MockRouterWithMiddleware 是支持显式中间件记录的路由器桩实现。
type MockRouterWithMiddleware struct {
	Routes []MiddlewareRoute
}

// NewMockRouterWithMiddleware 创建一个支持中间件记录的 mock 路由器。
func NewMockRouterWithMiddleware() *MockRouterWithMiddleware {
	return &MockRouterWithMiddleware{
		Routes: make([]MiddlewareRoute, 0),
	}
}

// GET 记录一条 GET 路由。
func (r *MockRouterWithMiddleware) GET(path string, handler httpx.Handler) httpx.IRouteGroup {
	r.Routes = append(r.Routes, MiddlewareRoute{
		Method:  "GET",
		Path:    path,
		Handler: handler,
	})
	return r
}

// POST 记录一条 POST 路由。
func (r *MockRouterWithMiddleware) POST(path string, handler httpx.Handler) httpx.IRouteGroup {
	r.Routes = append(r.Routes, MiddlewareRoute{
		Method:  "POST",
		Path:    path,
		Handler: handler,
	})
	return r
}

// PUT 记录一条 PUT 路由。
func (r *MockRouterWithMiddleware) PUT(path string, handler httpx.Handler) httpx.IRouteGroup {
	r.Routes = append(r.Routes, MiddlewareRoute{
		Method:  "PUT",
		Path:    path,
		Handler: handler,
	})
	return r
}

// DELETE 记录一条 DELETE 路由。
func (r *MockRouterWithMiddleware) DELETE(path string, handler httpx.Handler) httpx.IRouteGroup {
	r.Routes = append(r.Routes, MiddlewareRoute{
		Method:  "DELETE",
		Path:    path,
		Handler: handler,
	})
	return r
}

// PATCH 记录一条 PATCH 路由。
func (r *MockRouterWithMiddleware) PATCH(path string, handler httpx.Handler) httpx.IRouteGroup {
	r.Routes = append(r.Routes, MiddlewareRoute{
		Method:  "PATCH",
		Path:    path,
		Handler: handler,
	})
	return r
}

// HEAD 记录一条 HEAD 路由。
func (r *MockRouterWithMiddleware) HEAD(path string, handler httpx.Handler) httpx.IRouteGroup {
	r.Routes = append(r.Routes, MiddlewareRoute{
		Method:  "HEAD",
		Path:    path,
		Handler: handler,
	})
	return r
}

// OPTIONS 记录一条 OPTIONS 路由。
func (r *MockRouterWithMiddleware) OPTIONS(path string, handler httpx.Handler) httpx.IRouteGroup {
	r.Routes = append(r.Routes, MiddlewareRoute{
		Method:  "OPTIONS",
		Path:    path,
		Handler: handler,
	})
	return r
}

// Group 在该 mock 中直接复用当前实例。
func (r *MockRouterWithMiddleware) Group(prefix string) httpx.IRouteGroup {
	return r
}

// Use 在该 mock 中保留为空实现。
func (r *MockRouterWithMiddleware) Use(middlewares ...httpx.Middleware) httpx.IRouteGroup {
	return r
}

// PrintRoutes 打印带中间件信息的路由列表。
func (r *MockRouterWithMiddleware) PrintRoutes() {
	log.Printf("registered routes with middleware:")
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
