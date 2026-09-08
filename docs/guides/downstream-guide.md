# 下游接入指南

业务项目负责业务规则和组合根装配；框架提供领域模板、查询协议、事件链路、授权及运行时能力。先按业务场景选择模块，再注入具体基础设施。

## 本地依赖

当前模块名为 `gochen`、`gochen-runtime`、`gochen-contrib`，使用 Go 1.27。采用平级目录布局：

```text
project/
├── gochen/
├── gochen-runtime/
├── gochen-contrib/       # 使用第三方适配器时需要
└── gochen-example/       # 业务应用示意
```

业务应用使用 Core 与 Runtime 时，`go.mod` 的本地依赖可写为：

```go.mod
module gochen-example

go 1.27.0

require (
    gochen v0.0.0
    gochen-runtime v0.0.0
)

replace gochen => ../gochen
replace gochen-runtime => ../gochen-runtime
```

仅使用 Core 时省略 Runtime；使用 Contrib 时增加对应 require 和本地 replace。`v0.0.0` 在这里是本地替换的占位版本，不是可下载的发布版本。完成 imports 后，在业务仓库执行 `go mod tidy` 整理其依赖。

联合开发可在父目录创建 `go.work`；已有 workspace 时使用 `go work use` 加入模块。workspace 方便联调，各仓库验证仍使用 `GOWORK=off`，确认各自的 `go.mod` 可以独立工作。发布依赖须使用真实模块版本并移除本地 replace。

## 选择应用模板

| 场景 | Core 入口 | 配套能力 |
| --- | --- | --- |
| 配置、字典、一般管理 | `domain/crud` + `app/crud` | Runtime Repo / REST |
| 需要审计、软删除与恢复 | `domain/audited` + `app/audited` | 审计 Store、同库事务 |
| 通过事件重建状态 | `domain/eventsourced` + `app/eventsourced` | EventStore、Outbox、Projection |
| 写入结果需要后续跟踪 | `app/operation` | 结果 Store、Tracker、轮询 / SSE |
| 多步骤补偿 | `process/saga` | 同步 CommandExecutor、状态 Store |
| 状态机与流程节点推进 | `process/workflow` | 定义与实例 Store |
| 后台任务与弹性控制 | `process/task` + `policy` | 受控生命周期、重试 / 限流 / 熔断 |

查询、日志、错误、ID 和上下文分别复用 `app/query`、`observe/logging`、`errors`、`gen`、`contextx`。Redis / NATS 等外部消息队列按 `messaging.ITransport` 接入。

## 组合根装配

组合根统一创建 DB、ORM、HTTP server、Transport、EventBus、Projection、日志和 ID 生成器，再通过构造函数或 options 注入。

- 常用路径使用 Runtime `quick`，详见 [Quick 装配](quick-assembly.md)。
- 有 provider、事件处理器、聚合 metadata 或投影注册的模块，使用 `gochen-runtime/host.Module(...).Provide(...).RouteRegistrar(...).Build()` 等 builder 能力。
- Host 运行配置通过 `gochen-runtime/host/config` 注入；模块契约位于 `host/module`。内部 assembly 与 runtime capability 供自定义 Host / adapter 集成使用。
- 配置结构化加载使用 Runtime `config.Provider.Load`，处理类型转换与校验错误。

Host 的健康路由挂在 BasePath 下；BasePath 为 `/api/v1` 时是 `/api/v1/healthz` 和 `/api/v1/readyz`。监控路由需显式启用 `WithEnableMonitoringRoutes(true)`，并在部署边界控制访问。

## CRUD、查询与 HTTP

标准 CRUD 使用 Runtime `api/rest` 注册路由。自定义 handler 也复用 `ParsePaginationOptions` / `ParseQueryParams`，使列表、过滤、排序和字段选择遵循同一份查询协议。

`app/query` 使用 `query` tag 或显式 `QuerySchema` 定义字段、类型和允许操作。空 `in` / `not_in` 列表返回错误；范围比较需要类型信息，缺少 schema 时不能假定字符串或数字语义。解析错误必须返回给调用方。

CRUD 扩展通过 `app/crud.Hooks` 注入，事务和审计要求见 [app](../../app/README.md)。请求体大小、严格 JSON 绑定和身份传递见 [httpx](../../httpx/README.md)，SQL 字段与表达式边界见 [db/orm](../../db/orm/README.md)。

## 安全接入

按需叠加动作权限、隔离、数据范围和治理能力。Runtime `security.Profile.Build` 装配受保护 Application，Repository 通过 `WithIsolation` / `WithScope` 显式声明安全列。HTTP、Worker、CLI 使用同一 Application。

构造期必须确认资源类型、范围声明和约束写能力；字段存在不等于安全能力已启用。跨隔离报表使用带留痕的 `WithCrossIsolationReads` 与 `contextx.WithCrossIsolation`，保持普通写入受限。模型、默认判定器与错误边界集中见[分层授权](../architecture/layered-authz.md)。

## 事件驱动接入

- 组合根显式创建并注入事件 Registry、UpgraderRegistry 和 ID generator。
- 消费边界通过 `upcast.HydrateEventPayload` / `DecodeEventPayload[T]` 升级并还原载荷。
- 可靠发布将事件与 Outbox 放在同一事务；消息 Transport 的成功语义须满足持久投递要求。
- checkpoint 模式要求事件流 Store，投影写入与 checkpoint 保存共用原子边界；消费者处理重复投递与重放。

装配过程见[事件溯源速查](../reference/ddd-eventsourcing-quick-reference.md)，表结构与索引要求见[数据库 Schema 与迁移](db-schema-migration-guide.md)。

## 验证

在业务仓库独立执行 build、vet、test；按采用的能力补充以下验证：

- 查询条件和分页与 schema 一致，非法输入返回明确错误。
- 受保护 Application 在 HTTP 之外仍执行授权；隔离与范围条件进入实际仓储查询。
- 审计和业务写同事务提交，失败路径不留下部分写入。
- 事件消费幂等，checkpoint 恢复与 Outbox 失败重试符合配置。
- 后台任务可停止，停止完成后再释放共享资源。

Runtime 示例位于该仓库的 `examples/domain/crud`、`examples/domain/audited`、`examples/domain/eventsourced`、`examples/quick`、`examples/security/layering`、`examples/infra/`。Core 示例见[示例索引](../../examples/README.md)，第三方适配见 [Contrib 指南](contrib-guide.md)。
