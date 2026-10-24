package testkit

import (
	"log"
	"sync"

	"gochen/httpx"
)

// RecordedRoute 描述一条由 RecordingRouter 捕获的路由。
type RecordedRoute struct {
	Method          string
	Path            string
	Handler         httpx.Handler
	MiddlewareCount int
}

type routeRecorder struct {
	mu     sync.RWMutex
	routes []RecordedRoute
}

// RecordingRouter 是只记录注册结果、不接收网络请求的路由组。
type RecordingRouter struct {
	recorder    *routeRecorder
	prefix      string
	middlewares []httpx.Middleware
}

// NewRecordingRouter 创建空的记录型路由器。
func NewRecordingRouter() *RecordingRouter {
	return &RecordingRouter{recorder: &routeRecorder{}}
}

// GET 记录 GET 路由。
func (r *RecordingRouter) GET(path string, handler httpx.Handler) httpx.IRouteGroup {
	r.record("GET", path, handler)
	return r
}

// POST 记录 POST 路由。
func (r *RecordingRouter) POST(path string, handler httpx.Handler) httpx.IRouteGroup {
	r.record("POST", path, handler)
	return r
}

// PUT 记录 PUT 路由。
func (r *RecordingRouter) PUT(path string, handler httpx.Handler) httpx.IRouteGroup {
	r.record("PUT", path, handler)
	return r
}

// DELETE 记录 DELETE 路由。
func (r *RecordingRouter) DELETE(path string, handler httpx.Handler) httpx.IRouteGroup {
	r.record("DELETE", path, handler)
	return r
}

// PATCH 记录 PATCH 路由。
func (r *RecordingRouter) PATCH(path string, handler httpx.Handler) httpx.IRouteGroup {
	r.record("PATCH", path, handler)
	return r
}

// HEAD 记录 HEAD 路由。
func (r *RecordingRouter) HEAD(path string, handler httpx.Handler) httpx.IRouteGroup {
	r.record("HEAD", path, handler)
	return r
}

// OPTIONS 记录 OPTIONS 路由。
func (r *RecordingRouter) OPTIONS(path string, handler httpx.Handler) httpx.IRouteGroup {
	r.record("OPTIONS", path, handler)
	return r
}

// Group 创建共享记录池的子路由组。
func (r *RecordingRouter) Group(prefix string) httpx.IRouteGroup {
	if r == nil {
		return NewRecordingRouter().Group(prefix)
	}
	return &RecordingRouter{
		recorder:    r.recorder,
		prefix:      httpx.JoinRoutePath(r.prefix, prefix),
		middlewares: append([]httpx.Middleware(nil), r.middlewares...),
	}
}

// Use 为后续在当前组注册的路由追加中间件。
func (r *RecordingRouter) Use(middlewares ...httpx.Middleware) httpx.IRouteGroup {
	if r != nil {
		r.middlewares = append(r.middlewares, middlewares...)
	}
	return r
}

// RecordedRoutes 返回当前记录的路由快照。
func (r *RecordingRouter) RecordedRoutes() []RecordedRoute {
	if r == nil || r.recorder == nil {
		return nil
	}
	r.recorder.mu.RLock()
	defer r.recorder.mu.RUnlock()
	return append([]RecordedRoute(nil), r.recorder.routes...)
}

// PrintRoutes 按注册顺序输出路由，便于示例展示。
func (r *RecordingRouter) PrintRoutes() {
	log.Printf("registered routes:")
	for _, route := range r.RecordedRoutes() {
		log.Printf("  %s %s", route.Method, route.Path)
	}
}

func (r *RecordingRouter) record(method, path string, handler httpx.Handler) {
	if r == nil {
		return
	}
	if r.recorder == nil {
		r.recorder = &routeRecorder{}
	}
	route := RecordedRoute{
		Method:          method,
		Path:            httpx.JoinRoutePath(r.prefix, path),
		Handler:         handler,
		MiddlewareCount: len(r.middlewares),
	}
	r.recorder.mu.Lock()
	r.recorder.routes = append(r.recorder.routes, route)
	r.recorder.mu.Unlock()
}
