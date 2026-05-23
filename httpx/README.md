# httpx/：HTTP 抽象与 `httpx/nethttp` 实现

`gochen/httpx` 的目标是提供**最小但足够的 HTTP 抽象**，让上层模块（尤其是 `gochen/api/rest`）不绑定具体 Web 框架；同时提供一个开箱即用的 `net/http` 实现：`gochen/httpx/nethttp`。

## 1. 顶层概念

| 概念 | 对应接口/类型 | 作用 |
|---|---|---|
| `IContext` | `httpx/context.go` | 处理器可见的上下文：读请求、写响应、存取键值、流程控制 |
| `IServer` / `IRouteGroup` | `httpx/server.go` | 注册路由、路由分组、全局/组级中间件 |
| `Middleware` | `httpx/server.go` | 统一的中间件签名：`func(ctx IContext, next func() error) error` |
| `IRequestContext` | `httpx/request.go` / `httpx/request_context.go` | 对 `context.Context` 的轻量封装（只负责派生；业务运行时语义走 `contextx`） |

gochen 的 HTTP 抽象采用接口隔离：请求读取、绑定、响应写入、上下文存取被拆分为多个小接口，再组合成 `IContext`（见 `httpx/context.go`）。

`IRequestContext` 的默认实现与构造函数位于 `gochen/httpx` 根包：

- `httpx.NewRequestContext(ctx)`
- `httpx.NewRequestContextWithValues(ctx, values)`

业务代码、测试桩和非 `net/http` 适配器应直接依赖上述根入口，不需要为了创建请求上下文 import `gochen/httpx/nethttp`。

## 2. `httpx/nethttp`（net/http）实现

`httpx/nethttp` 提供一个基于标准库的实现：

- 服务器：`nethttp.NewServer(*httpx.WebConfig)`（见 `httpx/nethttp/http_server.go`）
- 上下文：`nethttp.Context`（见 `httpx/nethttp/context.go`）
- 路由：Go 1.22+ `ServeMux` 的 `"METHOD /path"` 模式；`:id` 会自动映射到 `{id}`（用于 `req.PathValue`）

`httpx/nethttp` 只负责 `net/http` server、route、response writer 与 base context 适配；协议无关的请求上下文默认实现不放在该包内。

### 最小示例：启动一个 HTTP 服务

```go
server := nethttp.NewServer(&httpx.WebConfig{Host: "0.0.0.0", Port: 8080})

server.GET("/healthz", func(c httpx.IContext) error {
	return c.JSON(200, map[string]any{"ok": true})
})

_ = server.Start(":8080")
```

### 路由分组与中间件

```go
api := server.Group("/api/v1").
	Use(authMiddleware)

api.GET("/users", listUsers)
api.POST("/users", createUser)
```

## 3. 与 `gochen/api/rest` 的关键约定

### 3.1 端点级请求体大小限制（`MaxBodySizeKey`）

`gochen/api/rest` 会在 handler 执行前通过 `ctx.Set(httpx.MaxBodySizeKey, limit)` 声明“最大 body 大小”。

- 这是一个“实现可选”的约定键（定义在 `httpx/request.go`）。
- `httpx/nethttp` 会在读取 Body 时使用 `http.MaxBytesReader` 强制限制（见 `httpx/nethttp/context.go`）。
- 安全默认（breaking）：当上层未显式设置 `MaxBodySizeKey` 时，`httpx/nethttp` 仍会启用默认上限 `httpx.DefaultMaxBodySizeBytes`（10MB）。
  - 放大限制：`ctx.Set(httpx.MaxBodySizeKey, 50<<20)`（例如 50MB）
  - 显式关闭限制（opt-out）：`ctx.Set(httpx.MaxBodySizeKey, int64(0))`

### 3.2 operator/租户等业务信息的传递

`IContext` 自带键值存取接口（`Set/Get`），推荐由鉴权中间件将“当前用户标识”等信息写入 ctx storage，再由上层（例如 `gochen/api/rest` 的 `RouteConfig.Audit.OperatorExtractor`）读取。

`IRequestContext` 本身不再暴露 `tenant/trace/request/session/user` getter：

- `tenant_id` / `trace_id` / `request_id` / `operator` / `user_id` / `session_id` 统一通过 `gochen/contextx` 读写；
- `client_ip` / `user_agent` 继续直接从 `IRequestReader` 读取。

### 3.3 JSON 绑定默认严格（breaking）

`httpx/nethttp` 的 `BindJSON`/`ShouldBindJSON` 默认采用严格解析策略：

- `DisallowUnknownFields`：出现未知字段时返回 `errors.InvalidInput`；
- 仅允许单一 JSON 值：禁止尾随数据或多值（例如 `{"a":1}{"a":2}`）。

若你依赖“宽松解析”（忽略未知字段/允许尾随），需要在上层自行实现宽松 binder。

### 3.4 CORS 安全默认（breaking）

`httpx/middleware` 的 CORS 中间件默认不开放跨域：

- `middleware.CORSFromWebConfig(nil)` 等价于 no-op；
- `cfg.CORSEnabled=false` 时 no-op；
- `cfg.CORSEnabled=true` 时要求显式 allowlist（`CORSAllowOrigins` 非空），否则仍视为 no-op。

如需显式允许任意 Origin（不安全，承接旧语义），请显式将 allowlist 设置为 `["*"]`（浏览器场景下不允许 credentials），例如：

```go
server.Use(middleware.CORS(&middleware.CORSConfig{
	AllowOrigins: []string{"*"},
}))
```

### 3.5 RateLimitConfig 结构体字面量（breaking）

`httpx/middleware` 的限流中间件 `middleware.RateLimit` 使用 `middleware.RateLimitConfig` 作为配置对象。

说明：`RateLimitConfig` 目前**内嵌**了 `gochen/policy/ratelimit.Config`，因此外部调用方若使用“带字段名”的结构体字面量，写法需要显式嵌套到 `Config: ...` 下，否则会编译失败。

示例：

```go
server.Use(middleware.RateLimit(middleware.RateLimitConfig{
	Config: ratelimit.Config{
		RequestsPerSecond: 50,
		BurstSize:         100,
	},
	SkipPaths: []string{"/healthz"},
}))
```

## 4. 扩展：适配其他 Web 框架

当你希望使用 Gin/Echo/Fiber 等框架时，可以按以下思路写适配层：

1) 实现 `gochen/httpx` 中的 `IContext`（可基于组合拆分接口实现）；
2) 实现 `IServer`/`IRouteGroup` 的路由注册与分组；
3) 保持中间件签名一致（`Middleware`），让 `gochen/api/rest` 等上层代码无需感知具体框架。

> 推荐策略：只在业务仓库实现适配层，gochen 侧保持抽象与 `httpx/nethttp` 的参考实现。
