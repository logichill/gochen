package httpx

import "context"

// IUnwrappableServer 表示包装型 IServer 可暴露其底层被包装的 IServer。
//
// 遵循标准库 errors.Unwrap 的“单层解包”惯例：调用方应循环解包，直到遇到
// 不再实现本接口的服务器为止，再对其做能力接口断言（如 nethttp 的
// IHandlerProvider / IServeMuxProvider）。
//
// 典型场景：WithRouteRegistry 返回的 routeRegistryServer 包装了真实服务器，
// 若不提供 Unwrap，底层扩展能力会被彻底遮蔽。
type IUnwrappableServer interface {
	Unwrap() IServer
}

// UnwrapServer 循环解包 IServer，返回最内层不再实现 IUnwrappableServer 的服务器。
// 传入 nil 时返回 nil；遇到自引用（Unwrap 返回自身）时立即停止，避免死循环。
func UnwrapServer(server IServer) IServer {
	for {
		unwrappable, ok := server.(IUnwrappableServer)
		if !ok {
			return server
		}
		inner := unwrappable.Unwrap()
		if inner == nil || inner == server {
			return server
		}
		server = inner
	}
}

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
