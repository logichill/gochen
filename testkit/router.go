package testkit

import (
	"context"
	"log"
	"strings"
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
		prefix:      joinRoutePath(r.prefix, prefix),
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
		Path:            joinRoutePath(r.prefix, path),
		Handler:         handler,
		MiddlewareCount: len(r.middlewares),
	}
	r.recorder.mu.Lock()
	r.recorder.routes = append(r.recorder.routes, route)
	r.recorder.mu.Unlock()
}

// RecordingServer 是实现 gochenhttp.IServer 的非阻塞记录型 server。
type RecordingServer struct {
	*RecordingRouter

	mu      sync.RWMutex
	started bool
	address string
}

// NewRecordingServer 创建不监听端口的记录型 server。
func NewRecordingServer() *RecordingServer {
	return &RecordingServer{RecordingRouter: NewRecordingRouter()}
}

// GET 记录 GET 路由。
func (s *RecordingServer) GET(path string, handler httpx.Handler) httpx.IServer {
	s.RecordingRouter.GET(path, handler)
	return s
}

// POST 记录 POST 路由。
func (s *RecordingServer) POST(path string, handler httpx.Handler) httpx.IServer {
	s.RecordingRouter.POST(path, handler)
	return s
}

// PUT 记录 PUT 路由。
func (s *RecordingServer) PUT(path string, handler httpx.Handler) httpx.IServer {
	s.RecordingRouter.PUT(path, handler)
	return s
}

// DELETE 记录 DELETE 路由。
func (s *RecordingServer) DELETE(path string, handler httpx.Handler) httpx.IServer {
	s.RecordingRouter.DELETE(path, handler)
	return s
}

// PATCH 记录 PATCH 路由。
func (s *RecordingServer) PATCH(path string, handler httpx.Handler) httpx.IServer {
	s.RecordingRouter.PATCH(path, handler)
	return s
}

// HEAD 记录 HEAD 路由。
func (s *RecordingServer) HEAD(path string, handler httpx.Handler) httpx.IServer {
	s.RecordingRouter.HEAD(path, handler)
	return s
}

// OPTIONS 记录 OPTIONS 路由。
func (s *RecordingServer) OPTIONS(path string, handler httpx.Handler) httpx.IServer {
	s.RecordingRouter.OPTIONS(path, handler)
	return s
}

// Use 为后续在根组注册的路由追加中间件。
func (s *RecordingServer) Use(middlewares ...httpx.Middleware) httpx.IServer {
	s.RecordingRouter.Use(middlewares...)
	return s
}

// Static 记录一条静态资源 GET 路由。
func (s *RecordingServer) Static(prefix, root string) httpx.IServer {
	_ = root
	s.RecordingRouter.GET(prefix, nil)
	return s
}

// ServeStatic 记录一条静态资源 GET 路由。
func (s *RecordingServer) ServeStatic(path, root string) {
	_ = root
	s.RecordingRouter.GET(path, nil)
}

// Start 记录启动地址并立即返回。
func (s *RecordingServer) Start(address string) error {
	s.mu.Lock()
	s.address = address
	s.started = true
	s.mu.Unlock()
	return nil
}

// Stop 标记 server 已停止并立即返回。
func (s *RecordingServer) Stop(ctx context.Context) error {
	if err := validateContext(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	s.started = false
	s.mu.Unlock()
	return nil
}

// IsStarted 返回 server 是否处于已启动状态。
func (s *RecordingServer) IsStarted() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.started
}

// Address 返回最近一次 Start 记录的地址。
func (s *RecordingServer) Address() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.address
}

func normalizeRoutePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || path == "/" {
		return path
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return strings.TrimSuffix(path, "/")
}

func joinRoutePath(prefix, path string) string {
	prefix = normalizeRoutePath(prefix)
	path = normalizeRoutePath(path)
	if prefix == "/" {
		prefix = ""
	}
	if path == "/" {
		path = ""
	}
	if prefix == "" {
		if path == "" {
			return "/"
		}
		return path
	}
	if path == "" {
		return prefix
	}
	return prefix + path
}

var (
	_ httpx.IRouteGroup = (*RecordingRouter)(nil)
	_ httpx.IServer     = (*RecordingServer)(nil)
)
