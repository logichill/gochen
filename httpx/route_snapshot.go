package httpx

// RegisteredRoute 描述驱动实际注册的唯一 method+path。
type RegisteredRoute struct {
	Method, Path string
	// AliasMethod 表示隐式方法由同路径的另一个方法处理器执行。
	AliasMethod string
	// AllowReason 标识驱动提供的公共协议入口，其处理器保持无资源访问。
	AllowReason string
}

// IRouteSnapshot 从实际注册源枚举路由，并在校验后冻结注册集合。
// 新增路由通过新的服务器实例重新装配、校验和启用。
type IRouteSnapshot interface {
	Routes() []RegisteredRoute
	SealRoutes() error
}

// IRouteLocation 暴露实际路径拼接，供入口绑定采用与驱动一致的稳定键。
type IRouteLocation interface {
	RoutePath(relative string) string
}
