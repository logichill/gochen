package module_test

import "gochen/httpx"

type mockRouteGroup struct {
	routes      []string
	middlewares []httpx.Middleware
	subGroups   []*mockRouteGroup
}

func newMockRouteGroup() *mockRouteGroup {
	return &mockRouteGroup{}
}

func (g *mockRouteGroup) GET(path string, handler httpx.Handler) httpx.IRouteGroup {
	g.routes = append(g.routes, "GET:"+path)
	return g
}
func (g *mockRouteGroup) POST(path string, handler httpx.Handler) httpx.IRouteGroup {
	g.routes = append(g.routes, "POST:"+path)
	return g
}
func (g *mockRouteGroup) PUT(path string, handler httpx.Handler) httpx.IRouteGroup {
	g.routes = append(g.routes, "PUT:"+path)
	return g
}
func (g *mockRouteGroup) DELETE(path string, handler httpx.Handler) httpx.IRouteGroup {
	g.routes = append(g.routes, "DELETE:"+path)
	return g
}
func (g *mockRouteGroup) PATCH(path string, handler httpx.Handler) httpx.IRouteGroup {
	g.routes = append(g.routes, "PATCH:"+path)
	return g
}
func (g *mockRouteGroup) HEAD(path string, handler httpx.Handler) httpx.IRouteGroup {
	g.routes = append(g.routes, "HEAD:"+path)
	return g
}
func (g *mockRouteGroup) OPTIONS(path string, handler httpx.Handler) httpx.IRouteGroup {
	g.routes = append(g.routes, "OPTIONS:"+path)
	return g
}
func (g *mockRouteGroup) Group(prefix string) httpx.IRouteGroup {
	sub := newMockRouteGroup()
	g.subGroups = append(g.subGroups, sub)
	return sub
}
func (g *mockRouteGroup) Use(middleware ...httpx.Middleware) httpx.IRouteGroup {
	g.middlewares = append(g.middlewares, middleware...)
	return g
}

type testService struct{ name string }

func NewTestService() *testService  { return &testService{name: "test"} }
func (s *testService) Name() string { return s.name }

type testRouteRegistrar struct{ service *testService }

func NewTestRouteRegistrar(svc *testService) *testRouteRegistrar {
	return &testRouteRegistrar{service: svc}
}

func (r *testRouteRegistrar) RegisterRoutes(group httpx.IRouteGroup) error {
	group.GET("/test", nil)
	group.POST("/test", nil)
	return nil
}
