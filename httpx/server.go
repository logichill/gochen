package httpx

import "context"

// IHttpServer HTTP 服务器接口
type IHttpServer interface {
	GET(path string, handler HttpHandler) IHttpServer
	POST(path string, handler HttpHandler) IHttpServer
	PUT(path string, handler HttpHandler) IHttpServer
	DELETE(path string, handler HttpHandler) IHttpServer
	PATCH(path string, handler HttpHandler) IHttpServer
	HEAD(path string, handler HttpHandler) IHttpServer
	OPTIONS(path string, handler HttpHandler) IHttpServer

	Group(prefix string) IRouteGroup
	Use(middleware ...Middleware) IHttpServer

	Static(prefix, root string) IHttpServer
	ServeStatic(path, root string)

	Start(addr string) error
	Stop(ctx context.Context) error

	RegisterRoute(method, path string, handler interface{})
	RegisterGroup(prefix string) IRouteGroup
	RegisterGlobalMiddleware(middleware interface{})
	RegisterMiddleware(path string, middleware interface{})

	HealthCheck() error
	GetRaw() interface{}
}

// Middleware 定义 HTTP 中间件签名
type Middleware func(ctx IHttpContext, next func() error) error

// IRouteGroup 定义路由组接口
type IRouteGroup interface {
	GET(path string, handler HttpHandler) IRouteGroup
	POST(path string, handler HttpHandler) IRouteGroup
	PUT(path string, handler HttpHandler) IRouteGroup
	DELETE(path string, handler HttpHandler) IRouteGroup
	PATCH(path string, handler HttpHandler) IRouteGroup
	HEAD(path string, handler HttpHandler) IRouteGroup
	OPTIONS(path string, handler HttpHandler) IRouteGroup

	Group(prefix string) IRouteGroup
	Use(middleware ...Middleware) IRouteGroup

	// 保留动态注册能力，兼容不同框架
	RegisterRoute(method, path string, handler interface{})
	RegisterMiddleware(middleware interface{})
}
