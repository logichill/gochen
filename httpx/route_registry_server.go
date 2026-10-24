package httpx

import (
	"context"
	"strings"
)

type routeRegistryServer struct {
	inner IServer
	reg   *routeRegistry
}

// GET 注册 GET 路由并记录注册信息（用于启动期冲突检测）。
func (s *routeRegistryServer) GET(path string, handler Handler) IServer {
	s.reg.add("GET", NormalizeRoutePath(path))
	if inner := s.inner.GET(path, handler); inner != nil {
		s.inner = inner
	}
	return s
}

// POST 注册 POST 路由并记录注册信息（用于启动期冲突检测）。
func (s *routeRegistryServer) POST(path string, handler Handler) IServer {
	s.reg.add("POST", NormalizeRoutePath(path))
	if inner := s.inner.POST(path, handler); inner != nil {
		s.inner = inner
	}
	return s
}

// PUT 注册 PUT 路由并记录注册信息（用于启动期冲突检测）。
func (s *routeRegistryServer) PUT(path string, handler Handler) IServer {
	s.reg.add("PUT", NormalizeRoutePath(path))
	if inner := s.inner.PUT(path, handler); inner != nil {
		s.inner = inner
	}
	return s
}

// DELETE 注册 DELETE 路由并记录注册信息（用于启动期冲突检测）。
func (s *routeRegistryServer) DELETE(path string, handler Handler) IServer {
	s.reg.add("DELETE", NormalizeRoutePath(path))
	if inner := s.inner.DELETE(path, handler); inner != nil {
		s.inner = inner
	}
	return s
}

// PATCH 注册 PATCH 路由并记录注册信息（用于启动期冲突检测）。
func (s *routeRegistryServer) PATCH(path string, handler Handler) IServer {
	s.reg.add("PATCH", NormalizeRoutePath(path))
	if inner := s.inner.PATCH(path, handler); inner != nil {
		s.inner = inner
	}
	return s
}

// HEAD 注册 HEAD 路由并记录注册信息（用于启动期冲突检测）。
func (s *routeRegistryServer) HEAD(path string, handler Handler) IServer {
	s.reg.add("HEAD", NormalizeRoutePath(path))
	if inner := s.inner.HEAD(path, handler); inner != nil {
		s.inner = inner
	}
	return s
}

// OPTIONS 注册 OPTIONS 路由并记录注册信息（用于启动期冲突检测）。
func (s *routeRegistryServer) OPTIONS(path string, handler Handler) IServer {
	s.reg.add("OPTIONS", NormalizeRoutePath(path))
	if inner := s.inner.OPTIONS(path, handler); inner != nil {
		s.inner = inner
	}
	return s
}

// Group 创建一个子路由组，并继承当前的路由注册记录器。
//
// prefix 规范化后透传给底层 server.Group，避免 "api/" 之类未剥离尾部斜杠的
// 前缀在底层字符串拼接出 "//users" 双斜杠路由，并保持与冲突检测表路径一致。
func (s *routeRegistryServer) Group(prefix string) IRouteGroup {
	prefix = NormalizeRoutePrefix(prefix)
	return &routeRegistryGroup{
		inner:  s.inner.Group(prefix),
		reg:    s.reg,
		prefix: prefix,
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
	p := NormalizeRoutePath(prefix)
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
	p := NormalizeRoutePath(path)
	s.reg.add("GET", p)
	s.reg.add("HEAD", p)
	s.inner.ServeStatic(path, root)
}

// Start 启动底层 server。
func (s *routeRegistryServer) Start(addr string) error { return s.inner.Start(addr) }

// Stop 停止底层 server。
func (s *routeRegistryServer) Stop(ctx context.Context) error { return s.inner.Stop(ctx) }

func (s *routeRegistryServer) RouteConflicts() []RouteConflict { return s.reg.conflicts() }

// Unwrap 返回被包装的底层服务器，使调用方可通过类型断言访问底层扩展能力
// （如 nethttp 的 IHandlerProvider / IServeMuxProvider）。
func (s *routeRegistryServer) Unwrap() IServer {
	if s == nil {
		return nil
	}
	return s.inner
}
