package httpx

import (
	"context"
	"testing"
)

type noopServer struct{}

func (s *noopServer) GET(string, Handler) IServer     { return s }
func (s *noopServer) POST(string, Handler) IServer    { return s }
func (s *noopServer) PUT(string, Handler) IServer     { return s }
func (s *noopServer) DELETE(string, Handler) IServer  { return s }
func (s *noopServer) PATCH(string, Handler) IServer   { return s }
func (s *noopServer) HEAD(string, Handler) IServer    { return s }
func (s *noopServer) OPTIONS(string, Handler) IServer { return s }
func (s *noopServer) Group(string) IRouteGroup        { return &noopGroup{} }
func (s *noopServer) Use(...Middleware) IServer       { return s }

func (s *noopServer) Static(string, string) IServer { return s }
func (s *noopServer) ServeStatic(string, string)    {}

func (s *noopServer) Start(string) error         { return nil }
func (s *noopServer) Stop(context.Context) error { return nil }
func (s *noopServer) HealthCheck() error         { return nil }

type noopGroup struct{}

func (g *noopGroup) GET(string, Handler) IRouteGroup     { return g }
func (g *noopGroup) POST(string, Handler) IRouteGroup    { return g }
func (g *noopGroup) PUT(string, Handler) IRouteGroup     { return g }
func (g *noopGroup) DELETE(string, Handler) IRouteGroup  { return g }
func (g *noopGroup) PATCH(string, Handler) IRouteGroup   { return g }
func (g *noopGroup) HEAD(string, Handler) IRouteGroup    { return g }
func (g *noopGroup) OPTIONS(string, Handler) IRouteGroup { return g }
func (g *noopGroup) Group(string) IRouteGroup            { return g }
func (g *noopGroup) Use(...Middleware) IRouteGroup       { return g }

func TestRouteRegistry_RecordsStaticAndServeStatic(t *testing.T) {
	srv := WithRouteRegistry(&noopServer{})
	rr, ok := srv.(IRouteRegistry)
	if !ok {
		t.Fatalf("expected IRouteRegistry, got %T", srv)
	}

	srv.Static("/static", "./a")
	srv.Static("/static", "./b")
	srv.ServeStatic("/favicon.ico", "./a")
	srv.ServeStatic("/favicon.ico", "./b")

	conflicts := rr.RouteConflicts()
	if !hasConflict(conflicts, "GET", "/static/", 2) {
		t.Fatalf("expected GET /static/ conflict count=2, got: %#v", conflicts)
	}
	if !hasConflict(conflicts, "GET", "/favicon.ico", 2) {
		t.Fatalf("expected GET /favicon.ico conflict count=2, got: %#v", conflicts)
	}
}

func hasConflict(conflicts []RouteConflict, method, path string, count int) bool {
	for _, c := range conflicts {
		if c.Method == method && c.Path == path && c.Count == count {
			return true
		}
	}
	return false
}
