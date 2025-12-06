package basic

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"

	httpx "gochen/http"
)

// HttpServer 基于标准库 net/http 的 IHttpServer 实现
type HttpServer struct {
	mux         *http.ServeMux
	config      *httpx.WebConfig
	server      *http.Server
	routes      map[string]*route
	groups      map[string]*RouteGroup
	middlewares []httpx.Middleware
	mu          sync.RWMutex
}

type route struct {
	method      string
	pattern     string
	handler     httpx.HttpHandler
	middlewares []httpx.Middleware
}

// NewHTTPServer 创建基于 net/http 的服务器
func NewHTTPServer(config *httpx.WebConfig) *HttpServer {
	return &HttpServer{
		mux:         http.NewServeMux(),
		config:      config,
		routes:      make(map[string]*route),
		groups:      make(map[string]*RouteGroup),
		middlewares: make([]httpx.Middleware, 0),
	}
}

// 路由注册实现
func (s *HttpServer) GET(path string, handler httpx.HttpHandler) httpx.IHttpServer {
	return s.addRoute("GET", path, handler)
}
func (s *HttpServer) POST(path string, handler httpx.HttpHandler) httpx.IHttpServer {
	return s.addRoute("POST", path, handler)
}
func (s *HttpServer) PUT(path string, handler httpx.HttpHandler) httpx.IHttpServer {
	return s.addRoute("PUT", path, handler)
}
func (s *HttpServer) DELETE(path string, handler httpx.HttpHandler) httpx.IHttpServer {
	return s.addRoute("DELETE", path, handler)
}
func (s *HttpServer) PATCH(path string, handler httpx.HttpHandler) httpx.IHttpServer {
	return s.addRoute("PATCH", path, handler)
}
func (s *HttpServer) HEAD(path string, handler httpx.HttpHandler) httpx.IHttpServer {
	return s.addRoute("HEAD", path, handler)
}
func (s *HttpServer) OPTIONS(path string, handler httpx.HttpHandler) httpx.IHttpServer {
	return s.addRoute("OPTIONS", path, handler)
}

func (s *HttpServer) addRoute(method, path string, handler httpx.HttpHandler) httpx.IHttpServer {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := method + " " + path
	s.routes[key] = &route{
		method:      method,
		pattern:     path,
		handler:     handler,
		middlewares: nil,
	}
	return s
}

// 路由分组
func (s *HttpServer) Group(prefix string) httpx.IRouteGroup {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := &RouteGroup{prefix: prefix, server: s, middlewares: make([]httpx.Middleware, 0)}
	s.groups[prefix] = g
	return g
}

// 全局中间件
func (s *HttpServer) Use(middleware ...httpx.Middleware) httpx.IHttpServer {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.middlewares = append(s.middlewares, middleware...)
	return s
}

// 静态文件
func (s *HttpServer) Static(prefix, root string) httpx.IHttpServer {
	fileServer := http.FileServer(http.Dir(root))
	s.mux.Handle(prefix, http.StripPrefix(prefix, fileServer))
	return s
}

func (s *HttpServer) ServeStatic(path, root string) {
	s.mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, root) })
}

// 启停
func (s *HttpServer) Start(addr string) error {
	if addr == "" {
		addr = fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)
	}
	s.registerRoutes()
	s.server = &http.Server{
		Addr:         addr,
		Handler:      s.mux,
		ReadTimeout:  s.config.ReadTimeout,
		WriteTimeout: s.config.WriteTimeout,
		IdleTimeout:  s.config.IdleTimeout,
	}
	if s.config.TLSEnabled {
		return s.server.ListenAndServeTLS(s.config.CertFile, s.config.KeyFile)
	}
	return s.server.ListenAndServe()
}

func (s *HttpServer) Stop(ctx context.Context) error {
	if s.server == nil {
		return nil
	}
	return s.server.Shutdown(ctx)
}

func (s *HttpServer) HealthCheck() error { return nil }
func (s *HttpServer) GetRaw() any        { return s.mux }

// 内部：注册全部路由
func (s *HttpServer) registerRoutes() {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, r := range s.routes {
		route := r // 捕获当前循环变量副本，避免闭包引用同一变量
		pattern := s.convertPathPattern(route.pattern)
		handler := s.createHandler(route)
		s.mux.HandleFunc(pattern, func(w http.ResponseWriter, req *http.Request) {
			if req.Method != route.method && route.method != "" { // 简单方法匹配
				if req.Method == http.MethodOptions {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			handler(w, req)
		})
	}
}

// 将 :id 转为 {id} （Go 1.22+ PathValue 支持）
func (s *HttpServer) convertPathPattern(pattern string) string {
	parts := strings.Split(pattern, "/")
	for i, p := range parts {
		if strings.HasPrefix(p, ":") {
			parts[i] = "{" + p[1:] + "}"
		}
	}
	return strings.Join(parts, "/")
}

func (s *HttpServer) createHandler(r *route) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		ctx := NewBaseHttpContext(w, req)
		s.parsePathParams(ctx, r.pattern, req)
		// 组装中间件链：全局 -> 路由级
		middlewares := append([]httpx.Middleware{}, s.middlewares...)
		if len(r.middlewares) > 0 {
			middlewares = append(middlewares, r.middlewares...)
		}
		// 执行
		if err := s.executeMiddlewareChain(ctx, middlewares, r.handler); err != nil {
			_ = (&HttpUtils{}).WriteErrorResponse(ctx, err)
		}
	}
}

func (s *HttpServer) parsePathParams(ctx *HttpContext, pattern string, req *http.Request) {
	parts := strings.Split(strings.Trim(pattern, "/"), "/")
	for _, part := range parts {
		if strings.HasPrefix(part, ":") {
			name := part[1:]
			if v := req.PathValue(name); v != "" {
				ctx.SetParam(name, v)
			}
		}
	}
}

func (s *HttpServer) executeMiddlewareChain(ctx httpx.IHttpContext, middlewares []httpx.Middleware, handler httpx.HttpHandler) error {
	if len(middlewares) == 0 {
		return handler(ctx)
	}
	return middlewares[0](ctx, func() error { return s.executeMiddlewareChain(ctx, middlewares[1:], handler) })
}

// RouteGroup 实现 IRouteGroup
type RouteGroup struct {
	prefix      string
	server      *HttpServer
	middlewares []httpx.Middleware
}

func (g *RouteGroup) GET(path string, h httpx.HttpHandler) httpx.IRouteGroup {
	return g.add("GET", path, h)
}
func (g *RouteGroup) POST(path string, h httpx.HttpHandler) httpx.IRouteGroup {
	return g.add("POST", path, h)
}
func (g *RouteGroup) PUT(path string, h httpx.HttpHandler) httpx.IRouteGroup {
	return g.add("PUT", path, h)
}
func (g *RouteGroup) DELETE(path string, h httpx.HttpHandler) httpx.IRouteGroup {
	return g.add("DELETE", path, h)
}
func (g *RouteGroup) PATCH(path string, h httpx.HttpHandler) httpx.IRouteGroup {
	return g.add("PATCH", path, h)
}
func (g *RouteGroup) HEAD(path string, h httpx.HttpHandler) httpx.IRouteGroup {
	return g.add("HEAD", path, h)
}
func (g *RouteGroup) OPTIONS(path string, h httpx.HttpHandler) httpx.IRouteGroup {
	return g.add("OPTIONS", path, h)
}

func (g *RouteGroup) Group(prefix string) httpx.IRouteGroup { return g.server.Group(g.prefix + prefix) }
func (g *RouteGroup) Use(mw ...httpx.Middleware) httpx.IRouteGroup {
	g.middlewares = append(g.middlewares, mw...)
	return g
}

func (g *RouteGroup) add(method, path string, h httpx.HttpHandler) httpx.IRouteGroup {
	full := g.prefix + path
	g.server.mu.Lock()
	defer g.server.mu.Unlock()
	g.server.routes[method+" "+full] = &route{method: method, pattern: full, handler: g.wrap(h), middlewares: nil}
	return g
}

func (g *RouteGroup) wrap(h httpx.HttpHandler) httpx.HttpHandler {
	return func(ctx httpx.IHttpContext) error { return g.server.executeMiddlewareChain(ctx, g.middlewares, h) }
}
