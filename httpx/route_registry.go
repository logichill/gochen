package httpx

import (
	"context"
	"sort"
	"strings"
	"sync"
)

// RouteConflict 表示 method+path 发生重复注册。
type RouteConflict struct {
	Method string
	Path   string
	Count  int
}

// IRouteRegistry 暴露路由注册的冲突信息（用于启动期 fail-fast 校验）。
type IRouteRegistry interface {
	RouteConflicts() []RouteConflict
}

func WithRouteRegistry(inner IServer) IServer {
	if inner == nil {
		return nil
	}
	if _, ok := inner.(IRouteRegistry); ok {
		return inner
	}
	return &routeRegistryServer{inner: inner, reg: newRouteRegistry()}
}

type routeRegistryServer struct {
	inner IServer
	reg   *routeRegistry
}

// GET 注册 GET 路由并记录注册信息（用于启动期冲突检测）。
func (s *routeRegistryServer) GET(path string, handler Handler) IServer {
	s.reg.add("GET", normalizeRoutePath(path))
	if inner := s.inner.GET(path, handler); inner != nil {
		s.inner = inner
	}
	return s
}

// POST 注册 POST 路由并记录注册信息（用于启动期冲突检测）。
func (s *routeRegistryServer) POST(path string, handler Handler) IServer {
	s.reg.add("POST", normalizeRoutePath(path))
	if inner := s.inner.POST(path, handler); inner != nil {
		s.inner = inner
	}
	return s
}

// PUT 注册 PUT 路由并记录注册信息（用于启动期冲突检测）。
func (s *routeRegistryServer) PUT(path string, handler Handler) IServer {
	s.reg.add("PUT", normalizeRoutePath(path))
	if inner := s.inner.PUT(path, handler); inner != nil {
		s.inner = inner
	}
	return s
}

// DELETE 注册 DELETE 路由并记录注册信息（用于启动期冲突检测）。
func (s *routeRegistryServer) DELETE(path string, handler Handler) IServer {
	s.reg.add("DELETE", normalizeRoutePath(path))
	if inner := s.inner.DELETE(path, handler); inner != nil {
		s.inner = inner
	}
	return s
}

// PATCH 注册 PATCH 路由并记录注册信息（用于启动期冲突检测）。
func (s *routeRegistryServer) PATCH(path string, handler Handler) IServer {
	s.reg.add("PATCH", normalizeRoutePath(path))
	if inner := s.inner.PATCH(path, handler); inner != nil {
		s.inner = inner
	}
	return s
}

// HEAD 注册 HEAD 路由并记录注册信息（用于启动期冲突检测）。
func (s *routeRegistryServer) HEAD(path string, handler Handler) IServer {
	s.reg.add("HEAD", normalizeRoutePath(path))
	if inner := s.inner.HEAD(path, handler); inner != nil {
		s.inner = inner
	}
	return s
}

// OPTIONS 注册 OPTIONS 路由并记录注册信息（用于启动期冲突检测）。
func (s *routeRegistryServer) OPTIONS(path string, handler Handler) IServer {
	s.reg.add("OPTIONS", normalizeRoutePath(path))
	if inner := s.inner.OPTIONS(path, handler); inner != nil {
		s.inner = inner
	}
	return s
}

// Group 创建一个子路由组，并继承当前的路由注册记录器。
func (s *routeRegistryServer) Group(prefix string) IRouteGroup {
	return &routeRegistryGroup{
		inner:  s.inner.Group(prefix),
		reg:    s.reg,
		prefix: normalizeRoutePrefix(prefix),
	}
}

// Use 为当前 server 追加中间件。
func (s *routeRegistryServer) Use(middleware ...Middleware) IServer {
	if inner := s.inner.Use(middleware...); inner != nil {
		s.inner = inner
	}
	return s
}

// Static 注册静态资源路由，并记录潜在冲突信息（method+path）。
func (s *routeRegistryServer) Static(prefix, root string) IServer {
	// 对齐 httpx/nethttp 的语义：Static 会注册 prefix 的重定向（当 prefix 不以 / 结尾时）
	// 以及真正的静态资源前缀（以 / 结尾的 pattern）。
	//
	// 这里用 method+path 记录，便于启动期 fail-fast 检测“重复注册”（即便底层实现不 panic）。
	p := normalizeRoutePath(prefix)
	if p == "" {
		p = "/"
	}
	pattern := p
	if !strings.HasSuffix(pattern, "/") {
		pattern += "/"
	}
	s.reg.add("GET", pattern)
	s.reg.add("HEAD", pattern)
	if p != pattern {
		s.reg.add("GET", p)
		s.reg.add("HEAD", p)
	}

	if inner := s.inner.Static(prefix, root); inner != nil {
		s.inner = inner
	}
	return s
}

// ServeStatic 提供静态资源服务，并记录潜在冲突信息（method+path）。
func (s *routeRegistryServer) ServeStatic(path, root string) {
	p := normalizeRoutePath(path)
	s.reg.add("GET", p)
	s.reg.add("HEAD", p)
	s.inner.ServeStatic(path, root)
}

// Start 启动底层 server。
func (s *routeRegistryServer) Start(addr string) error { return s.inner.Start(addr) }

// Stop 停止底层 server。
func (s *routeRegistryServer) Stop(ctx context.Context) error { return s.inner.Stop(ctx) }

// HealthCheck 委托给底层 server 执行健康检查。
func (s *routeRegistryServer) HealthCheck() error { return s.inner.HealthCheck() }

func (s *routeRegistryServer) RouteConflicts() []RouteConflict { return s.reg.conflicts() }

type routeRegistryGroup struct {
	inner  IRouteGroup
	reg    *routeRegistry
	prefix string
}

// GET 注册 GET 路由并记录注册信息（用于启动期冲突检测）。
func (g *routeRegistryGroup) GET(path string, handler Handler) IRouteGroup {
	g.reg.add("GET", joinRoutePath(g.prefix, path))
	if inner := g.inner.GET(path, handler); inner != nil {
		g.inner = inner
	}
	return g
}

// POST 注册 POST 路由并记录注册信息（用于启动期冲突检测）。
func (g *routeRegistryGroup) POST(path string, handler Handler) IRouteGroup {
	g.reg.add("POST", joinRoutePath(g.prefix, path))
	if inner := g.inner.POST(path, handler); inner != nil {
		g.inner = inner
	}
	return g
}

// PUT 注册 PUT 路由并记录注册信息（用于启动期冲突检测）。
func (g *routeRegistryGroup) PUT(path string, handler Handler) IRouteGroup {
	g.reg.add("PUT", joinRoutePath(g.prefix, path))
	if inner := g.inner.PUT(path, handler); inner != nil {
		g.inner = inner
	}
	return g
}

// DELETE 注册 DELETE 路由并记录注册信息（用于启动期冲突检测）。
func (g *routeRegistryGroup) DELETE(path string, handler Handler) IRouteGroup {
	g.reg.add("DELETE", joinRoutePath(g.prefix, path))
	if inner := g.inner.DELETE(path, handler); inner != nil {
		g.inner = inner
	}
	return g
}

// PATCH 注册 PATCH 路由并记录注册信息（用于启动期冲突检测）。
func (g *routeRegistryGroup) PATCH(path string, handler Handler) IRouteGroup {
	g.reg.add("PATCH", joinRoutePath(g.prefix, path))
	if inner := g.inner.PATCH(path, handler); inner != nil {
		g.inner = inner
	}
	return g
}

// HEAD 注册 HEAD 路由并记录注册信息（用于启动期冲突检测）。
func (g *routeRegistryGroup) HEAD(path string, handler Handler) IRouteGroup {
	g.reg.add("HEAD", joinRoutePath(g.prefix, path))
	if inner := g.inner.HEAD(path, handler); inner != nil {
		g.inner = inner
	}
	return g
}

// OPTIONS 注册 OPTIONS 路由并记录注册信息（用于启动期冲突检测）。
func (g *routeRegistryGroup) OPTIONS(path string, handler Handler) IRouteGroup {
	g.reg.add("OPTIONS", joinRoutePath(g.prefix, path))
	if inner := g.inner.OPTIONS(path, handler); inner != nil {
		g.inner = inner
	}
	return g
}

// Group 创建一个子路由组，并继承当前的路由注册记录器。
func (g *routeRegistryGroup) Group(prefix string) IRouteGroup {
	return &routeRegistryGroup{
		inner:  g.inner.Group(prefix),
		reg:    g.reg,
		prefix: joinRoutePrefix(g.prefix, prefix),
	}
}

// Use 为当前 group 追加中间件。
func (g *routeRegistryGroup) Use(middleware ...Middleware) IRouteGroup {
	if inner := g.inner.Use(middleware...); inner != nil {
		g.inner = inner
	}
	return g
}

type routeKey struct {
	method string
	path   string
}

type routeRegistry struct {
	mu     sync.Mutex
	counts map[routeKey]int
}

// newRouteRegistry 创建路由注册表。
func newRouteRegistry() *routeRegistry {
	return &routeRegistry{counts: make(map[routeKey]int, 128)}
}

// add 累计 (METHOD, normalizedPath) 注册次数；用于 conflicts 检测重复路由。
func (r *routeRegistry) add(method, path string) {
	method = strings.TrimSpace(strings.ToUpper(method))
	path = normalizeRoutePath(path)
	if method == "" || path == "" {
		return
	}

	r.mu.Lock()
	r.counts[routeKey{method: method, path: path}]++
	r.mu.Unlock()
}

// conflicts 返回所有注册次数 > 1 的路由（按 method+path 维度认定为冲突）。
func (r *routeRegistry) conflicts() []RouteConflict {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]RouteConflict, 0)
	for k, c := range r.counts {
		if c > 1 {
			out = append(out, RouteConflict{Method: k.method, Path: k.path, Count: c})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path == out[j].Path {
			return out[i].Method < out[j].Method
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// normalizeRoutePrefix 规范化路由前缀。
func normalizeRoutePrefix(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" || prefix == "/" {
		return ""
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	return strings.TrimSuffix(prefix, "/")
}

// normalizeRoutePath 规范化路由路径。
func normalizeRoutePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}

func joinRoutePrefix(parentPrefix string, childPrefix string) string {
	parentPrefix = normalizeRoutePrefix(parentPrefix)
	childPrefix = normalizeRoutePrefix(childPrefix)
	if parentPrefix == "" {
		return childPrefix
	}
	if childPrefix == "" {
		return parentPrefix
	}
	return parentPrefix + childPrefix
}

func joinRoutePath(prefix string, path string) string {
	prefix = normalizeRoutePrefix(prefix)
	path = normalizeRoutePath(path)
	if prefix == "" {
		return path
	}
	if path == "" || path == "/" {
		return prefix
	}
	return prefix + path
}
