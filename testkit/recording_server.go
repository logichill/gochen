package testkit

import (
	"context"
	"sync"

	"gochen/httpx"
)

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

var (
	_ httpx.IRouteGroup = (*RecordingRouter)(nil)
	_ httpx.IServer     = (*RecordingServer)(nil)
)
