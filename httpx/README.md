# httpx/：HTTP 接口契约与抽象

`gochen/httpx` 属于 Core 模块，提供**框架无关的 HTTP 核心抽象**（请求读取、响应写入、上下文存取、中间件签名与统一响应助手），使领域编排与应用层不绑定任何具体 Web 框架。

> 物理驱动实现位于 Runtime 模块：`gochen-runtime` 仓库 `http/nethttp`（基于 Go 标准库 `net/http`），说明见该仓库 `http/README.md`。

## 1. 顶层概念

| 概念 | 对应接口/类型 | 作用 |
|---|---|---|
| `IContext` | `httpx/context.go` | 处理器可见的上下文：读请求、写响应、存取键值、流程控制 |
| `IServer` / `IRouteGroup` | `httpx/server.go` | 注册路由、路由分组、全局/组级中间件契约 |
| `Middleware` | `httpx/server.go` | 统一中间件签名：`func(ctx IContext, next func() error) error` |
| `IRequestContext` | `httpx/request.go` | 对 `context.Context` 的轻量封装（业务运行时语义走 `contextx`） |

gochen 的 HTTP 抽象采用接口隔离：请求读取、绑定、响应写入、上下文存取被拆分为多个小接口，再组合成 `IContext`（见 `httpx/context.go`）。

`IRequestContext` 的默认实现与构造函数位于 `gochen/httpx` 根包：

- `httpx.NewRequestContext(ctx)`
- `httpx.NewRequestContextWithValues(ctx, values)`

业务代码、测试桩和自定义适配器应直接依赖上述 Core 根入口。

## 2. 统一响应助手（Helper）

`httpx` 提供统一的 JSON 响应写出助手，保证全框架 API 风格一致：

- 成功响应：`httpx.WriteSuccess(c, data)`、`httpx.WriteCreated(c, data)`、`httpx.WriteAccepted(c, data)`、`httpx.WriteNoContent(c)`
- 错误响应：`httpx.WriteError(c, status, msg)`、`httpx.WriteErrorCode(c, code, msg)`、`httpx.WriteErrorExtra(c, status, code, msg, details)`

## 3. 标准实现与启动示例（`gochen-runtime/http`）

Runtime 模块提供了开箱即用的标准库实现：

```go
import (
    "gochen/httpx"
    "gochen-runtime/http"
    "gochen-runtime/http/nethttp"
)

server := nethttp.NewServer(&http.WebConfig{Host: "0.0.0.0", Port: 8080})

server.GET("/healthz", func(c httpx.IContext) error {
    return httpx.WriteSuccess(c, map[string]any{"ok": true})
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

## 3. 与 `gochen-runtime/api/rest` 的关键约定

### 3.1 端点级请求体大小限制（`MaxBodySizeKey`）

`gochen-runtime/api/rest` 会在 handler 执行前通过 `ctx.Set(httpx.MaxBodySizeKey, limit)` 声明“最大 body 大小”。

- 这是一个“实现可选”的约定键（定义在 `httpx/request.go`）。
- `httpx/nethttp` 会在读取 Body 时使用 `http.MaxBytesReader` 强制限制（见 `httpx/nethttp/context.go`）。
- 安全默认（breaking）：当上层未显式设置 `MaxBodySizeKey` 时，`httpx/nethttp` 仍会启用默认上限 `httpx.DefaultMaxBodySizeBytes`（10MB）。
  - 放大限制：`ctx.Set(httpx.MaxBodySizeKey, 50<<20)`（例如 50MB）
  - 0 或负数会回退到默认上限；如需上传大文件，请显式设置足够大的正数上限。

### 3.2 operator/租户等业务信息的传递

`IContext` 自带键值存取接口（`Set/Get`），推荐由鉴权中间件将“当前用户标识”等信息写入 ctx storage，再由上层（例如 `gochen-runtime/api/rest` 的 `RouteConfig.Audit.OperatorExtractor`）读取。

`IRequestContext` 本身不再暴露 `tenant/trace/request/session/user` getter：

- `tenant_id` / `trace_id` / `request_id` / `operator` / `user_id` / `session_id` 统一通过 `gochen/contextx` 读写；
- `client_ip` / `user_agent` 继续直接从 `IRequestReader` 读取。

### 3.3 JSON 绑定语义

`httpx/nethttp` 的 `BindJSON` 默认采用“拒绝未知字段、禁止尾随数据”的严格解析策略：

- 未声明字段会直接返回 `errors.InvalidInput`，避免请求体拼写错误被静默吞掉；
- 仅允许单一 JSON 值：尾随数据或多值（例如 `{"a":1}{"a":2}`）返回 `errors.InvalidInput`。

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
3) 保持中间件签名一致（`Middleware`），让 `gochen-runtime/api/rest` 等上层代码无需感知具体框架。

> 推荐策略：只在业务仓库实现适配层，gochen 侧保持抽象与 `httpx/nethttp` 的参考实现。
