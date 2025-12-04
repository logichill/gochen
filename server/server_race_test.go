package server

import (
	"context"
	"sync"
	"testing"
	"time"

	"gochen/app"
	"gochen/di"
	httpx "gochen/http"
)

// minimalHTTPServer 为并发测试提供的 IHttpServer 实现，只记录 Start/Stop 调用。
type minimalHTTPServer struct {
	mu         sync.Mutex
	started    bool
	stopped    bool
	startCalls int
	stopCalls  int
}

func (s *minimalHTTPServer) GET(_ string, _ httpx.HttpHandler) httpx.IHttpServer    { return s }
func (s *minimalHTTPServer) POST(_ string, _ httpx.HttpHandler) httpx.IHttpServer   { return s }
func (s *minimalHTTPServer) PUT(_ string, _ httpx.HttpHandler) httpx.IHttpServer    { return s }
func (s *minimalHTTPServer) DELETE(_ string, _ httpx.HttpHandler) httpx.IHttpServer { return s }
func (s *minimalHTTPServer) PATCH(_ string, _ httpx.HttpHandler) httpx.IHttpServer  { return s }
func (s *minimalHTTPServer) HEAD(_ string, _ httpx.HttpHandler) httpx.IHttpServer   { return s }
func (s *minimalHTTPServer) OPTIONS(_ string, _ httpx.HttpHandler) httpx.IHttpServer {
	return s
}

func (s *minimalHTTPServer) Group(_ string) httpx.IRouteGroup            { return &noopRouteGroup{} }
func (s *minimalHTTPServer) Use(_ ...httpx.Middleware) httpx.IHttpServer { return s }
func (s *minimalHTTPServer) Static(_, _ string) httpx.IHttpServer        { return s }
func (s *minimalHTTPServer) ServeStatic(_, _ string)                     {}
func (s *minimalHTTPServer) HealthCheck() error                          { return nil }
func (s *minimalHTTPServer) GetRaw() any                                 { return nil }

func (s *minimalHTTPServer) Start(_ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.startCalls++
	s.started = true
	return nil
}

func (s *minimalHTTPServer) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopCalls++
	s.stopped = true
	return nil
}

type noopRouteGroup struct{}

func (g *noopRouteGroup) GET(_ string, _ httpx.HttpHandler) httpx.IRouteGroup    { return g }
func (g *noopRouteGroup) POST(_ string, _ httpx.HttpHandler) httpx.IRouteGroup   { return g }
func (g *noopRouteGroup) PUT(_ string, _ httpx.HttpHandler) httpx.IRouteGroup    { return g }
func (g *noopRouteGroup) DELETE(_ string, _ httpx.HttpHandler) httpx.IRouteGroup { return g }
func (g *noopRouteGroup) PATCH(_ string, _ httpx.HttpHandler) httpx.IRouteGroup  { return g }
func (g *noopRouteGroup) HEAD(_ string, _ httpx.HttpHandler) httpx.IRouteGroup   { return g }
func (g *noopRouteGroup) OPTIONS(_ string, _ httpx.HttpHandler) httpx.IRouteGroup {
	return g
}

func (g *noopRouteGroup) Group(_ string) httpx.IRouteGroup            { return g }
func (g *noopRouteGroup) Use(_ ...httpx.Middleware) httpx.IRouteGroup { return g }

// TestServer_WithEngine_ConcurrentSafe 验证 Server 与 Engine 在并发场景下无竞态。
func TestServer_WithEngine_ConcurrentSafe(t *testing.T) {
	modules := []app.IModule{} // 使用空模块集，避免引入额外依赖

	httpServer := &minimalHTTPServer{}
	container := di.NewBasic()

	srv := NewServer(modules,
		WithServerContainer(container),
		WithServerHTTPServer(httpServer),
	)

	engine := NewEngine(srv,
		WithShutdownTimeout(50*time.Millisecond),
	)

	if err := engine.Start(); err != nil {
		t.Fatalf("Engine.Start with Server returned error: %v", err)
	}

	if engine.State() != StateStopped {
		t.Fatalf("expected engine state %v, got %v", StateStopped, engine.State())
	}

	httpServer.mu.Lock()
	defer httpServer.mu.Unlock()
	if httpServer.startCalls == 0 || httpServer.stopCalls == 0 {
		t.Fatalf("expected HTTP server Start/Stop to be called at least once, got startCalls=%d stopCalls=%d",
			httpServer.startCalls, httpServer.stopCalls)
	}
	if !httpServer.started || !httpServer.stopped {
		t.Fatalf("expected HTTP server started/stopped flags to be true, got started=%v stopped=%v", httpServer.started, httpServer.stopped)
	}
}
