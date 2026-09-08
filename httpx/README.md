# httpx：HTTP 契约与响应助手

Core `gochen/httpx` 提供请求读取、绑定、响应写入、上下文、路由和中间件契约。标准服务器与请求解析在 `gochen-runtime` 仓库 `http/`，Gin 适配器在 `gochen-contrib` 仓库 `http/gin/`。

## 接口

| 入口 | 职责 |
| --- | --- |
| `IContext` | 组合请求、绑定、响应、键值存取和流程控制能力 |
| `IRequestContext` | 包装并派生 `context.Context` |
| `IServer` / `IRouteGroup` | 路由、中间件、分组与服务生命周期 |
| `Handler` | `func(IContext) error` |
| `Middleware` | `func(IContext, func() error) error` |
| `IFileHandler` / `IUploadedFile` | 可选文件上传能力 |

`NewRequestContext(ctx)` 与 `NewRequestContextWithValues(ctx, values)` 返回 `(IRequestContext, error)`，应处理构造错误。tenant、operator、user、session、trace、request 通过 `gochen/contextx` 读写；客户端 IP 与 User-Agent 从请求接口读取。

## 响应助手

- 成功：`WriteSuccess(ctx, data)`、`WriteCreated(ctx, data)`、`WriteAccepted(ctx, data)`、`WriteNoContent(ctx)`。
- 错误：`WriteError(ctx, err)`、`WriteErrorExtra(ctx, err, extra)`、`WriteErrorCode(ctx, code, message)`。
- 响应模型为 `ResponseMessage`，统一携带 code / message 及可选 data / details / extra / trace_id / request_id。
- `WriteRedirectJSON` 只接受以 `/` 开头的站内路径，拒绝外部 URL 和协议相对 URL。

路由注册示例：

```go
package example

import "gochen/httpx"

func RegisterHealth(group httpx.IRouteGroup) {
	group.GET("/healthz", func(ctx httpx.IContext) error {
		return httpx.WriteSuccess(ctx, map[string]any{"ok": true})
	})
}
```

完整服务装配见 [Quick 指南](../docs/guides/quick-assembly.md)及 `gochen-runtime` 仓库 `http/README.md`。

## 请求边界

`MaxBodySizeKey` 是端点请求体限制的约定键。Runtime REST 通过 `ctx.Set(httpx.MaxBodySizeKey, limit)` 设置，具体 HTTP 适配器负责执行。Runtime nethttp 在未设置正数限制时使用 `DefaultMaxBodySizeBytes`（10 MiB），0 或负数不会关闭限制。

Runtime nethttp 的 `BindJSON` 拒绝未知字段、空请求体和尾随 JSON 数据。手写 handler 与 REST 应共享该解析语义；需要其他协议时由业务显式选择解码方式。

Core `TenantContextMiddleware` 与 Runtime tenant middleware 都要求显式 resolver。租户来源应是已认证身份或可信网关；HTTP 中间件不替代 [Application 授权](../docs/architecture/layered-authz.md)。

Runtime `http/middleware` 提供 CORS、限流与熔断。CORS 默认关闭，启用时须配置非空 allowlist；通配 Origin 不能与 credentials 组合。限流、熔断参数复用 [policy](../policy/README.md)。

## 自定义适配器

适配器实现上述 Core 接口并保留 context 取消、错误映射、请求体限制和中间件顺序。Runtime REST 只依赖这些契约。访问具体 net/http 对象时使用 Runtime `http/nethttp` 的受限 helper，业务 Application 保持对 HTTP 实现无感知。
