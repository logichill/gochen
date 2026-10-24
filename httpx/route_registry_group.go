package httpx

type routeRegistryGroup struct {
	inner  IRouteGroup
	reg    *routeRegistry
	prefix string
}

// GET 注册 GET 路由并记录注册信息（用于启动期冲突检测）。
func (g *routeRegistryGroup) GET(path string, handler Handler) IRouteGroup {
	g.reg.add("GET", JoinRoutePath(g.prefix, path))
	if inner := g.inner.GET(path, handler); inner != nil {
		g.inner = inner
	}
	return g
}

// POST 注册 POST 路由并记录注册信息（用于启动期冲突检测）。
func (g *routeRegistryGroup) POST(path string, handler Handler) IRouteGroup {
	g.reg.add("POST", JoinRoutePath(g.prefix, path))
	if inner := g.inner.POST(path, handler); inner != nil {
		g.inner = inner
	}
	return g
}

// PUT 注册 PUT 路由并记录注册信息（用于启动期冲突检测）。
func (g *routeRegistryGroup) PUT(path string, handler Handler) IRouteGroup {
	g.reg.add("PUT", JoinRoutePath(g.prefix, path))
	if inner := g.inner.PUT(path, handler); inner != nil {
		g.inner = inner
	}
	return g
}

// DELETE 注册 DELETE 路由并记录注册信息（用于启动期冲突检测）。
func (g *routeRegistryGroup) DELETE(path string, handler Handler) IRouteGroup {
	g.reg.add("DELETE", JoinRoutePath(g.prefix, path))
	if inner := g.inner.DELETE(path, handler); inner != nil {
		g.inner = inner
	}
	return g
}

// PATCH 注册 PATCH 路由并记录注册信息（用于启动期冲突检测）。
func (g *routeRegistryGroup) PATCH(path string, handler Handler) IRouteGroup {
	g.reg.add("PATCH", JoinRoutePath(g.prefix, path))
	if inner := g.inner.PATCH(path, handler); inner != nil {
		g.inner = inner
	}
	return g
}

// HEAD 注册 HEAD 路由并记录注册信息（用于启动期冲突检测）。
func (g *routeRegistryGroup) HEAD(path string, handler Handler) IRouteGroup {
	g.reg.add("HEAD", JoinRoutePath(g.prefix, path))
	if inner := g.inner.HEAD(path, handler); inner != nil {
		g.inner = inner
	}
	return g
}

// OPTIONS 注册 OPTIONS 路由并记录注册信息（用于启动期冲突检测）。
func (g *routeRegistryGroup) OPTIONS(path string, handler Handler) IRouteGroup {
	g.reg.add("OPTIONS", JoinRoutePath(g.prefix, path))
	if inner := g.inner.OPTIONS(path, handler); inner != nil {
		g.inner = inner
	}
	return g
}

// Group 创建一个子路由组，并继承当前的路由注册记录器。
// Group 创建一个子路由组，并继承当前的路由注册记录器。
//
// 子前缀规范化后透传给底层 group.Group（底层链路自身已持有父前缀，
// 只接收子前缀避免双重拼接）；冲突检测表则记录拼接后的完整前缀，
// 并避免未剥离尾部斜杠的前缀在底层拼接出双斜杠路由。
func (g *routeRegistryGroup) Group(prefix string) IRouteGroup {
	childPrefix := NormalizeRoutePrefix(prefix)
	return &routeRegistryGroup{
		inner:  g.inner.Group(childPrefix),
		reg:    g.reg,
		prefix: JoinRoutePrefix(g.prefix, childPrefix),
	}
}

// Use 为当前 group 追加中间件。
func (g *routeRegistryGroup) Use(middleware ...Middleware) IRouteGroup {
	if inner := g.inner.Use(middleware...); inner != nil {
		g.inner = inner
	}
	return g
}
