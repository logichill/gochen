package httpx

import "context"

// IServer HTTP 服务器接口。
type IServer interface {
	GET(path string, handler Handler) IServer
	POST(path string, handler Handler) IServer
	PUT(path string, handler Handler) IServer
	DELETE(path string, handler Handler) IServer
	PATCH(path string, handler Handler) IServer
	HEAD(path string, handler Handler) IServer
	OPTIONS(path string, handler Handler) IServer

	Group(prefix string) IRouteGroup
	Use(middleware ...Middleware) IServer

	Static(prefix, root string) IServer
	ServeStatic(path, root string)

	Start(addr string) error
	Stop(ctx context.Context) error

	HealthCheck() error
}

// Middleware 定义 HTTP 中间件签名。
type Middleware func(ctx IContext, next func() error) error

// IRouteGroup 定义路由组接口。
type IRouteGroup interface {
	GET(path string, handler Handler) IRouteGroup
	POST(path string, handler Handler) IRouteGroup
	PUT(path string, handler Handler) IRouteGroup
	DELETE(path string, handler Handler) IRouteGroup
	PATCH(path string, handler Handler) IRouteGroup
	HEAD(path string, handler Handler) IRouteGroup
	OPTIONS(path string, handler Handler) IRouteGroup

	Group(prefix string) IRouteGroup
	Use(middleware ...Middleware) IRouteGroup
}
