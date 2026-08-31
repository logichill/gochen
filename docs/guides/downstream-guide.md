# 下游项目接入与治理指南

面向在业务仓库里落地 gochen 的读者。全文聚焦四件事：**gochen 提供什么能力**、**该走哪条路**、**哪些边界不能绕**、**评审时该看什么**。

## 1. 模块能力一览

下表是挑选模块的出发点。凡是跨项目复用、与业务语义弱相关的能力，优先从 gochen 找；业务规则、业务策略、业务 adapter 才是下游项目自己的事。

| 能力域 | 主要模块 | 提供什么 |
| --- | --- | --- |
| 领域建模 | `domain` | 实体接口、聚合基类、仓储端口、领域约束基线 |
| 应用模板 | `app/crud`、`app/audited`、`app/eventsourced` | CRUD / audited / event sourced 应用服务 |
| 写操作协议 | `app/operation` | 对外可观察的写操作结果、状态跟踪、SSE / 轮询入口 |
| 生命周期与装配 | `host`、`di` | 进程生命周期、模块契约、组合根装配 |
| HTTP 与 API | `httpx`、`api/rest` | 框架无关 HTTP 抽象、REST CRUD 路由、统一响应 |
| 数据访问 | `db` | Query DSL、ORM 抽象、SQL Builder、方言与安全边界 |
| 事件驱动 | `eventing`、`eventing/upcast` | EventStore、Outbox、Projection、Subscription、Monitoring、事件载荷升级 |
| 消息通信 | `messaging` | MessageBus、Transport、CommandBus、中间件、DLQ |
| 过程运行时 | `process/saga`、`process/workflow`、`process/lock` | 补偿编排、状态机推进、业务 key 串行化 |
| 控制策略 | `policy` | 重试、限流、熔断 |
| 授权与错误 | `auth/{action,scoped,governance}`、`app/security`、`gochen-runtime/security`、`host/authz`、`auth/sqlstore`、`errors` | 渐进安全能力、标准装配、权限目录、治理存储与错误码体系 |
| 后台与观测 | `task`、`observe`、`logging` | 后台 goroutine 监督、日志/指标/追踪依赖聚合 |
| 运行时基础 | `contextx`、`validate`、`clock`、`config`、`codec`、`gen`、`cache` | 上下文语义、校验、时间、配置、编解码、ID 策略、泛型缓存 |

留在业务项目的能力：具体业务实体与规则、业务专属 adapter 与外部集成、业务流程编排、组织/资源级策略判定、项目特有 UI/API 契约。

## 2. 先选对路径

按业务复杂度从上到下挑。没拿准的情况下选更简单那档，以后真需要再往下升。

| 场景 | 起点 |
| --- | --- |
| 简单管理域 | `domain/crud` + `app/crud` + `api/rest` |
| 审计型管理域（操作人、软删、恢复） | `domain/audited` + `app/audited` + `api/rest` |
| 关键交易域 / 事件驱动域 | `domain/eventsourced` + `app/eventsourced` + `eventing/*` + `messaging/*` |
| 以上任一 + 租户/授权边界 | 叠加 `security.Profile` + `db/orm/repo` 的显式 isolation/scoped 能力 |
| 写后结果延迟可见 / 前端需要跟踪进度 | 叠加 `app/operation`，只在需要 `accepted/processing/settled` 等可见状态时启用 |
| 后台循环 / 定时补偿 / 长驻任务 | 叠加 `task.TaskSupervisor`，不要裸起无人监管的 goroutine |

一个反向信号：如果你已经在手写大量列表 handler、审计分支、事件发布器或 tenant/权限判断，不要继续堆业务代码——先回头确认这部分是不是 gochen 已经覆盖了。

## 3. 最小组合根接入骨架

下游项目推荐从 `host.Module(...)` + `host.Run(...)` 开始。业务模块只声明自己提供什么，组合根负责创建基础设施并注入 Host。

模块声明骨架：

```go
func NewInventoryModule() (host.IModule, error) {
	return host.Module("inventory").
		Name("Inventory").
		Provide(
			NewInventoryRepository,
			NewInventoryService,
		).
		RouteRegistrar(NewInventoryRoutes).
		Extension(authz.Catalog{PermissionDefinitions: InventoryPermissions()}).
		Build()
}
```

进程启动骨架：

```go
func main() {
	ctx := context.Background()

	httpServer := buildHTTPServer()
	eventBus := buildEventBus()
	projectionManager := buildProjectionManager()

	if err := host.Run(ctx,
		hostconfig.WithName("erp"),
		hostconfig.WithHTTPServer(httpServer),
		hostconfig.WithEventBus(eventBus),
		hostconfig.WithProjectionRuntime(projectionManager),
		hostconfig.WithBasePath("/api/v1"),
		hostconfig.WithModuleHTTP("inventory", hostconfig.ModuleHTTPConfig{Prefix: "/inventory"}),
		hostconfig.WithModules(
			iam.NewModule,
			inventory.NewInventoryModule,
			order.NewModule,
		),
		hostconfig.WithFailFastOnRouteConflicts(true),
	); err != nil {
		log.Fatal(err)
	}
}
```

这段骨架表达三条约束：

- `main` / bootstrap 是基础设施唯一创建点；DB、HTTP server、event bus、projection runtime、transport、ID 生成器都在这里确定，并按能力选择 `host/config` option、provider 构造函数或 repo/application option 注入。
- 业务模块通过 `Provide(...)`、`RouteRegistrar(...)` 声明基础能力，通过 `Extension(authz.Catalog{...})` 声明可选权限目录，不直接构造 Host 底层 descriptor / registration / role。
- 普通业务模块优先通过构造函数拿依赖；只有编写高级模块集成、adapter 或框架扩展时，才直接使用 `host/module/runtimecap` 的 typed accessor 读取 HTTP、authz、event bus、projection 等运行时能力。

入口分层上，业务组合根优先依赖运行时包 `gochen-runtime/host`；模块作者只需要 `gochen-runtime/host/module` 的契约类型；`host/module/runtime`、`host/module/assembly`、`host/module/runtimecap` 和 `host/module/initcap` 保留给框架内部、高级 adapter 或自定义 Host 集成。

上面只展示启动骨架；真实项目里的 DB、validator、ID generator 等业务基础设施通常通过 provider 构造函数、DI 注册或 repo/application option 注入。HTTP server、transport、event bus、projection runtime 等 Host 运行时能力才走对应 `hostconfig.WithXxx(...)` option。

完整可运行示例见 `gochen-runtime` 仓库 `examples/host/module_boot`。

## 4. 几条不能绕的主线

下面这些点在接入时容易走偏，事后补代价很大，值得在一开始就对齐。

### 组合根是依赖的唯一真相源

DB、HTTP server、message transport、event store、validator、ID 生成器——这些基础设施依赖必须在组合根显式创建，通过接口注入下游。handler / service / repo 内部**不要 `new` 基础设施**。

生命周期走 `host.Run(...)` / `host.New(...)`；模块声明用 `host.Module(...)` 收敛 provider、路由、事件处理器、投影注册。Host 运行时能力通过 `gochen-runtime/host/config` 的 `hostconfig.WithXxx(...)` 从组合根注入，业务基础设施通过 provider 构造函数或 repo/application option 注入；普通业务模块不要直接接触 descriptor、registration、role，也不要直接依赖 `host/module/runtime` 或 `host/module/assembly`；`host/module/runtimecap` 只留给高级模块集成、adapter 或框架扩展。

Host 默认在 BasePath group 下注册 `GET /healthz` 与 `GET /readyz`；例如 BasePath 为 `/api/v1` 时实际路径是 `/api/v1/healthz` 与 `/api/v1/readyz`。`/metrics`、`/snapshot` 同样挂在 BasePath 下，且涉及运行指标与健康快照，必须由组合根显式启用 `hostconfig.WithEnableMonitoringRoutes(true)`，并在网关、鉴权或网络边界上保护。

### 标准 CRUD 走 builder，不要手写一整套 HTTP 样板

路由注册、分页过滤、统一响应这些东西 `gochen-runtime/api/rest` 已经做完了。只有接口明显超出标准 CRUD 语义时才建议手写 handler；即便手写，查询协议也继续复用 `rest.ParsePaginationOptions(...)` / `rest.ParseQueryParams(...)` 之类的入口，不要在项目内部长出第二套 `page/size/filter/sorts/fields` 规则。

### CRUD 扩展统一走 `Hooks`

标准 CRUD 的写入扩展逻辑通过 `app/crud.Hooks` 注册，通常在组合根、路由装配或模块 builder 里用 `app.SetHooks(...)`、`rest.WithHooks(...)` 或 builder `Hooks(...)` 注入。不要通过嵌入 `Application` 后定义 `BeforeCreate` / `AfterCreate` 等方法来扩展 CRUD；这些方法不再是扩展点。

Hook 阶段由框架固定：`Before*` 在写入前执行，失败阻断写入；`After*` 在写入后、提交前执行，失败回滚；`PostCommit*` 在提交后执行，失败不回滚。需要事务后副作用（通知、外部同步、异步任务触发）优先放 `PostCommit*`。


### 升级提示：安全分层与 SQLBuilder 空 slice

原 `auth` 根包、`domain/access`、`auth/http` 与 CRUD tenant wrapper 已删除，不提供兼容垫片。动作、范围与治理契约分别迁移到 `auth/action`、`auth/scoped`、`auth/governance`；组合根使用 `gochen-runtime/security.Profile.Build` 固化 Application 装饰顺序。

L2 直接从 `contextx.TenantID` 取隔离标识，并要求实体实现 `domain/crud.ITenantEntity`；仓储必须在构造期显式声明 `WithIsolation(...)`。L3 还必须声明 `WithResourceKind(...)` 与 `WithScope(...)`，缺少资源种类、范围列或写约束消费能力时在安全装饰器构造期失败。

上面两条的迁移风险是**静默失效**，升级时必须逐仓储核对：旧版按字段名（`tenant_id` / `managed_scope_id`）隐式推断并自动激活安全列，现已取消。未显式调用 `WithIsolation` / `WithScope` 的仓储，其隔离与范围过滤会直接不生效——编译通过、测试通过、防护消失，没有任何报错。同理，`api/rest` 已不再承担授权，未经 `security.Profile.Build` 装饰的 Application 从任何入口调用都完全无鉴权。核对方式：对每个原先依赖隐式推断的实体，确认仓储构造处有对应的 `WithIsolation` / `WithScope` 声明，并用一个跨隔离空间的读用例断言过滤真的生效。

HTTP Action 中间件迁移到 `gochen-runtime/api/rest/action.FastDeny`，仅用于提前拒绝，不是权威安全边界；HTTP、Worker、CLI 等入口都必须调用已装饰的 Application。批量写约束由 L3 Application 逐项判定并通过受控 context 通道下推，Repo 在同一事务的最终写 SQL 中原子求交。

`httpx.TenantContextMiddleware(resolver)` / `httpx/nethttp.TenantMiddleware(resolver)` 现在必须传入各自适配层的显式 tenant resolver。公网 API 应从已鉴权 principal、session 或 token 推导 tenant；只有在可信网关已经完成身份校验并重写请求头时，才在 `httpx/nethttp` 适配层显式传 `nethttp.TrustedTenantHeaderResolver` 读取 `X-Tenant-ID`。不要把客户端可控 header 当作默认租户来源。

`db/sql/sqlbuilder` 对 `WHERE id IN ?` 这类 slice 占位符会展开为 `IN (?,...)`；空 slice/array 会改写为 `0=1`，表示“空集合返回空结果”，避免生成非法 SQL。

`app/query` 和 `gochen-runtime/api/rest` 对 `in` / `not_in` 的空值列表按错误处理，不再静默跳过过滤器。下游如果把空集合当作“不过滤”，应在构造 `Filter` 或请求参数前自行分支。

`db/orm/repo` 的 `FilterOpLike` 只接受 string/enum typed value，会转义 `%`、`_` 和 `\`，并生成 `ESCAPE '\'`，语义是“包含这段字面文本”而不是开放 SQL 通配符。确实需要受信任通配符时，不要复用用户输入构造 `FilterOpLike`，应在仓储侧显式选择可信 SQL 表达式。

无 `QuerySchema` 的适配层范围查询现在会返回 `InvalidInput`：`app/query.DecodeAdapterFilters(...)` 的返回值从 `QueryFilters` 变为 `(QueryFilters, error)`，`FilterBuilder.Apply(...)` 也会返回 error。下游直接调用时必须处理错误；需要 `gt/gte/lt/lte` 这类范围查询时，应配置 `QuerySchema` 让框架做类型感知解码。

`api/rest.AppendPaginationFilters` / `AppendQueryFilters` 新增 `context.Context` 首参，并在无 schema 回退解码时写入诊断日志。下游调用点改为传入请求上下文；没有请求上下文时可传 `context.Background()`，但推荐沿用当前 handler/application ctx。

`httpx/nethttp.Context.BindJSON` 采用严格 JSON 绑定：未知字段、尾随数据、空 body 都会返回 `InvalidInput`。下游手写 handler 如果需要宽松 JSON，应在业务 handler 中显式选择自定义解码逻辑，不要复用框架默认绑定入口。

`httpx` 的 CORS 默认关闭，`CORSEnabled=false` 或 `CORSAllowOrigins` 为空都不会开放跨域；启动装配 CORS 中间件时会输出诊断告警。需要保留旧的开放行为时，显式设置 `cors_enabled: true` 与 `cors_allow_origins: ["*"]`，且不要同时开启 credentials。

`app/operation.NewRunner` / `DefaultRunner` 返回导出的 `*operation.Runner`，该类型仍实现 `operation.IRunner`。Runner 完全接管 `Operation.ID/Type/Mode/Status`；业务 handler 只返回 `Resource/Result/Error/AffectedScopes`。handler 返回 error 时，Runner 会同时返回 failed envelope 和原 error。下游如果用具体未导出类型或依赖构造函数返回接口，需要改为接收 `*operation.Runner` 或显式赋值给 `operation.IRunner`。

`eventing/store.IEventStore` 的聚合级方法统一以 `(aggregateType, aggregateID)` 定位事件流。自定义 store、decorator 和测试 mock 必须让 Append/Load/Exists/Version 全部接收非空 aggregate type。`eventing.NewEvent` 要求显式传入 `gen.IGenerator[string]` 并处理错误；`DomainEventStoreOptions.EventIDGenerator` 未配置时会为该 store 实例创建独立 UUID generator，不再读取包级默认生成器。需要十进制 Snowflake ID 时，在组合根调用 `gen.NewSnowflakeStringGenerator(datacenterID, workerID)` 并显式注入；多实例部署必须为不同节点配置不同参数。

`auth/scoped.NewAuthorizer` 只组合资源解析器与评估器，不隐式创建策略或默认放行。需要快照一致性、决策 ID、审计或指标时，用 `auth/governance.Wrap` 显式装饰；传入 nil clock / ID generator 会在构造期失败。

`config.Provider.Bind` 已更名为 `config.Provider.Load`，签名从 `Bind(key string, target any) error` 变为 `Load(key string, target any, opts ...LoadOption) error`；`LoadOption` 支持 `WithImplicitEnvironment`、`WithPostBind`、`WithValidation` 等扩展。升级时将所有 `.Bind(...)` 调用改为 `.Load(...)`；不需要新选项时直接省略变参即可。

`config.Provider.Load` 对已存在配置值使用严格类型转换；缺失 key 仍保留结构体默认值，但类型不匹配、非整数浮点写入整数、溢出等都会返回错误。升级时要把字符串/布尔/数字配置的来源类型整理清楚，避免依赖旧的零值降级。

Saga 的 `OnComplete` 回调失败会把 Saga 标记为可恢复的 `pending_completion`，并发布 `EventSagaCompletionFailed`；后续 `Resume` 会重试完成回调。`OnComplete` 应保持幂等；框架会在成功回调后写入内部标记，避免恢复路径重复执行。

权限目录已迁移到 `gochen-runtime/host/authz`。组合根显式创建 `*authz.Registry`，通过 `hostconfig.WithCatalogRegistrar(authz.NewCatalogRegistrar(registry))` 注入 Host；模块用 `Extension(authz.Catalog{...})` 声明目录，避免默认全局 registry。

Host 运行时注入接口已收窄到 `host/capability.I*`，其中 `ITransport` 只要求 `Subscribe`、`Start`、`Stop` 这组模块运行时所需能力；只有需要直接作为消息总线传输实现复用时，才同时满足完整 `messaging.ITransport`。组合根和测试桩应实现对应 capability 接口；迁移 runner 的具体类型使用导出的 `*migrate.Runner`。

`capability.IProjectionManager` 已重命名为 `capability.IProjectionRuntime`，同时原 `capability.IProjectionStarter` 接口（`StartProjection` 方法）已合入其中。自定义投影运行时适配器需将实现类型改为 `IProjectionRuntime` 并补充 `StartProjection(name string) error` 方法；原来同时实现 `IProjectionManager` + `IProjectionStarter` 的代码只需保留一个 `IProjectionRuntime` 即可。

`examples/internal/mocks` 包已移除，测试 mock 能力迁移至 `testkit` 包：`mocks.NewMockServer()` → `testkit.NewRecordingServer()`，`mocks.NewMockRepository()` → `testkit.NewMemoryRepository[T]()`，`mocks.NewMockRouter()` → `testkit.NewRecordingRouter()`。下游测试如果直接引用了 `examples/internal/mocks`，需改为依赖 `testkit`。

### 查询协议统一走 `app/query`

列表、过滤、排序、字段投影都建立在 `app/query` 上。优先顺序：`query` tag 自动推导 → 显式 `QuerySchema` → 自定义 handler 复用同一解析入口。目标是让 API、repo、测试共享同一份 schema 语义，而不是同一个项目里并存"v1 / v2 / 某模块私有"三套协议。

示例：`gochen-runtime` 仓库 `examples/domain/query/inferred`、`examples/domain/query/filter`。


### 安全能力包路径约定

`gochen/auth` 仅作为命名空间根：L1/L3/L4 契约分别位于 `auth/action`、`auth/scoped`、`auth/governance`。标准 Application 装配位于 `gochen-runtime/security`，HTTP Fast-Deny 位于 `gochen-runtime/api/rest/action`，治理 SQL store 位于 `gochen-runtime/auth/sqlstore`；Host 权限目录与**默认判定实现**（`NewActionChecker` / `NewEvaluator`）位于 `gochen-runtime/host/authz`——判定实现放在 runtime 侧而不是契约包，因为它依赖 `Principal`，且不该让 L0/L1 使用者背上一套具体 RBAC 模型。L2 不设转发包，直接复用 `contextx.TenantID` 与 `domain/crud.ITenantEntity`（跨隔离开闸凭据同样在 `contextx`）。

### 授权边界收口到统一链路

tenant / 资源授权 / 写入保护**不要在 handler、service、repo 各自长一套**。标准链路是：

- Transport AuthN 把 tenant / operator 等身份字段写入 `contextx`；
- `security.Profile.Build` 固化 Action 与 Scoped Application 装饰顺序；
- Scoped 装饰器完成 PDP 判定，并把 `auth/scoped.WriteConstraint` 与 `DataScope` 放入受控 context；
- 显式启用 `WithIsolation` / `WithScope` 的 Repo 将隔离、范围、资源与 revision 条件原子合并到最终 SQL。

设计背景见 [`architecture/rbac-to-layered-authz.md`](../architecture/rbac-to-layered-authz.md)。

### 判定器可以不自己写

`security.Profile` 要求你提供 `IActionChecker`（L1）与 `IAuthorizer`（L3），但**判定规则本身有默认实现**，不必从 `EvaluatorFunc` 手写起：

```go
authorizer, err := authscoped.NewAuthorizer(resourceResolver, authz.NewEvaluator())

profile := security.Profile[*Order, int64]{
    ActionChecker: authz.NewActionChecker(),   // runtime/host/authz
    Authorizer:    authorizer,
    Policy:        authaction.OperationPolicy{Create: "order:api:create", /* ... */},
    EntityType:    "order",
}
```

默认实现的规则：

- 动作校验就是 `Principal.AllowsPermission`——`IsSystem` 直接放行，显式持有或按段通配 `order:api:*` 命中即放行；
- 列表读（无具体目标）动作通过即放行，可见范围随后由仓储的 DataScope 过滤决定；
- 有目标时逐个资源核对隔离归属与 `managed_scope_id` 归属，**一条越界即整批拒绝**；
- 数据范围来源依次是：ctx 上已绑定的 `DataScope` → `IsSystem` 视为全局 → `Principal.ActiveScopeID` 单范围 → 拒绝。要表达"整棵子树可见"（`VisibleScopes`）必须显式传 `authz.WithEvaluatorDataScope(resolver)`。

三条容易踩的边界，都是刻意的 fail-closed：

- 资源的 `ManagedScopeID` 为 0 一律拒绝。**Create 场景请让 `IResourceResolver` 显式给出目标范围**——框架不会拿"你当前唯一可见的那个范围"替你填上，因为那是在替业务决定数据落在哪儿；
- `GlobalScope: true` 的平台级资源只对可见范围为全局的主体开放，否则租户内的 update 权限能改到平台配置；
- 权限码不符合三段式返回 `InvalidInput` 而不是 403——拼错是装配错误，不该伪装成权限不足。

接 OPA / Casbin / SpiceDB 时换掉 `IEvaluator` 即可，`security.Profile` 与仓储侧装配都不用动。

### 跨隔离空间读取走正门，不要再开第二个仓储

对账、计费汇总、跨租户报表、数据迁移、平台巡检确实需要越过 L2 隔离。**不要**为此在同一张表上再构造一个不带 `WithIsolation` 的仓储，也不要注入 `*sql.DB` 手写 SQL——前者形态与普通仓储完全一致、无痕且读写通吃，后者还会连带丢掉软删除过滤与字段映射。正门是两道闸：

```go
// 1) 构造期：声明这张表有被开闸的资格，并交出留痕出口（nil 会在 NewRepo fail-fast）
repo, err := ormrepo.NewRepo[*Order, int64](orm, "orders",
    ormrepo.WithIsolation[*Order, int64](ormrepo.IsolationCols{Column: "tenant_id"}),
    ormrepo.WithCrossIsolationReads[*Order, int64](ormrepo.CrossIsolationAuditFunc(
        func(ctx context.Context, read ormrepo.CrossIsolationRead) error {
            return auditStore.Append(ctx, read)   // 转接到 governance / 日志 / 指标
        })),
)

// 2) 请求期：带上"谁为了什么"，两者都必填
ctx, err := contextx.WithCrossIsolation(ctx, contextx.CrossIsolation{
    Reason:   "monthly_billing_reconciliation",
    Operator: "platform_cron_worker",
})
orders, err := repo.ListAll(ctx)   // 跨全部隔离空间，并留下一条痕迹
```

必须知道的边界：

- **缺任何一道闸都按常规隔离处理**：凭据落到未声明选项的仓储上完全惰性，声明了选项的仓储在无凭据请求上依然严格隔离；
- **只覆盖读**。已开闸仓储上的 `Create/Update/Delete/Purge/*All` 一律返回 `Forbidden`，跨隔离写请按目标隔离键逐个派生 context；
- **留痕失败即拒绝本次读取**，不会降级成普通隔离读；
- 资源边界解析（`ResolveResourceByID*`）永不放行——它是 L3 判定的输入；
- `WithRequireScope` 仓储若只有隔离一条边界，开闸时会因"没有可执行边界"被拒，这是预期行为；
- testkit 内存仓储有等价选项 `WithMemoryCrossIsolationReads`，两侧按同一组断言测试（`cross_isolation_test.go`）。

可运行示例：`gochen-runtime` 仓库 `examples/security/layering` 的 `demoCrossIsolation()`——覆盖"普通请求仍隔离 / 开闸跨全部空间 / 留痕内容 / 写被拒 / 凭据单独无效"五条。

### 事件/消息/Outbox/Projection 不要各造轮子

进入事件驱动场景就直接用 `eventing/store`、`eventing/outbox`、`eventing/projection`、`messaging`。如果项目已经开始写"事件表 + 异步补偿 + 自定义发布器 + 读模型同步器"，通常是误判了 gochen 的覆盖范围，不是 gochen 不够。

事件链路有几条硬约束：

- 消费边界统一走 `eventing/upcast.HydrateEventPayload(...)` / `DecodeEventPayload[T](...)` 做 payload 升级与强类型 hydration，不要在投影、handler、repo 里手写 `PayloadValue -> UpgradeEventData -> DeserializeFromMap`。
- SQL event store 的 `global_position` 是迁移负责的 schema 能力：建列时必须创建单列唯一索引，并在 `event_store_positions` 写入对应 `store_name` 的 `next_position` 分配行；写路径会校验这些 schema 约束，缺失时 fail-fast，不会运行期自动补 DDL。
- 启用 Projection checkpoint 时必须配置 `IEventStreamStore`；投影处理与 checkpoint 保存要处于同一原子边界，普通投影用 `projection.NewCheckpointingProjector(...)` 包装。
- 在线处理、checkpoint 追赶、显式恢复、重建由 `ProjectionManager` 的 per-projection runtime 串行化；业务不要再给同一投影自建并发调度。
- Outbox SQL claim/mark 依赖 claim token + lease；发布器、并行发布器和 SQL repository 都应使用框架 normalize 后的配置，不要只靠 `status=pending` 自己抢任务。
- 自定义 `messaging.IMessage` 进入异步 transport 时必须实现 `messaging.IMessageEnvelopeCloner`，或显式声明 `messaging.IImmutableMessageEnvelope`；否则 memory transport 会拒绝发布，避免快照/重投时丢失 concrete type。

示例：`gochen-runtime` 仓库 `examples/infra/outbox/sql`、`examples/infra/projection/sql_checkpoint`、`examples/infra/snapshot/basic`。

### 写操作状态用 `app/operation`，过程推进才用 `process`

如果一次写操作返回成功后，读模型还没收敛，或者前端/上游需要看到 `accepted`、`processing`、`settled`、`failed` 等状态，用 `app/operation` 包装 application service 写入口。它解决的是"这次写操作现在到哪了"，不是新的业务命令分发框架。

`process/saga` / `process/workflow` / `process/lock` 只在确实存在跨步骤补偿、状态机推进或按业务 key 串行化时使用。不要因为用了 CommandBus、CQRS 或事件溯源，就默认把所有写操作升级成 tracked operation。

使用 `process/workflow.AdvanceNodeTo` 表达选择分支时，只会走被选中的直接后继，且不再校验该出边上的 `Condition`——显式选择意味着决策责任在调用方。多入边节点必须显式声明 `Kind`：join-all 使用 `NodeKindJoin`，“任一分支到达即可继续”的 exclusive merge 使用 `NodeKindTask`，避免基于图结构猜测导致实例长期 pending。

配置条件路由时，某个节点的出边不能全是条件边：引擎无法证明条件互斥且穷尽，业务数据落在所有条件之外会让实例无处可去。用 `NewDefaultEdge` 显式声明 else 分支（每节点最多一条），否则 `SaveDefinition` 会直接拒绝该定义。运行期若仍出现"无出边命中"，引擎返回 `Conflict` 而不会把实例静默标记为 completed。

`process/workflow` 只管理流程推进到哪一步，**不执行**节点业务逻辑——没有动作执行 SPI。节点该做什么，用 `engine.OnNodeEnter` / `OnNodeExit` 注册节点级钩子来承接，或由调用方在推进前后自行编排；钩子返回错误会中止整次状态迁移。`NodeHookContext.Data` 与 `TransitionContext.Data` 都是快照，需要改实例数据必须走 `*WithMutation` 入口的 `StateMutation`。

超时能力分两层：`CheckTimeouts(instanceID)` 点检已知实例；`ScanTimeouts(limit)` 供后台巡检"发现"超时实例，要求 store 额外实现 `IQueryableStore`（`gochen-runtime/process/workflow/sqlstore.SQLStore` 已实现，`MemoryStore` 亦已实现）。两者只检测不处置，催办/自动通过/终止等策略由业务决定。生产环境与任何多实例部署必须使用实现了 `IOptimisticStore` 的存储——引擎的进程内 keyed lock 不能替代跨进程并发控制。

`process/workflow.State.Data` 与 `process/saga.SagaState.Data` 建议只存放 JSON-like 数据（`map[string]any`、`[]any`、字符串、数字、布尔、`[]byte` 等）。框架会为常见可变容器走快速深拷贝；自定义复杂 slice/map/数组或指向它们的指针会进入反射兜底，语义上仍会隔离可变底层数据，但不适合高频大对象拷贝。若下游必须存放自定义复杂类型，应在写入前转换成稳定快照，或在业务侧提供明确的克隆/持久化边界。

### 后台任务用 `task` 管起来

长驻循环、定时补偿、读模型修复、外部系统同步等后台逻辑不要裸起 goroutine。统一用 `task.TaskSupervisor` 接管生命周期、panic 恢复、失败记录和停止等待；重试/限流/熔断分别接 `policy/retry`、`policy/ratelimit`、`policy/circuit`。

### 通用运行时语义统一入口

不要在 HTTP 层、消息层、repo 层各自定义一套 tenant / operator / user / session / principal 语义。tenant / operator / user / session 与 trace / request / tx scope / metadata propagation 统一放在 `contextx`；动作、范围与治理分别复用 `auth/action`、`auth/scoped`、`auth/governance`；错误码统一走 `errors`；ID 生成走 `gen`；配置走 `config`；校验走 `validate`；缓存优先用 `cache`；可观测依赖用 `observe` / `logging` 显式注入，不要再封一层全局单例。

`propagation.MapCarrier` 是 `map[string]string` 的轻量传播载体，零值 `var metadata propagation.MapCarrier` 是 nil map，调用 `Set` 会 panic。需要可写 metadata 时使用 `propagation.NewMapCarrier()` 或 map literal（如 `propagation.MapCarrier{}`）；迁移旧代码时优先替换零值声明，避免链路字段注入时触发运行期 panic。

## 5. 最常见的偏离方式

| 反模式 | 典型表现 | 推荐做法 |
| --- | --- | --- |
| 生命周期装配散落 | 在 `main`、handler、service 里各自创建 DB、router、transport | 收口到 `gochen-runtime/host` / 组合根 |
| 手写第二套 CRUD 框架 | 标准增删改查也重写路由注册、分页协议、统一响应 | 走 `app/crud`、`app/audited`、`gochen-runtime/api/rest` |
| 查询协议碎片化 | 各模块自定义 `pageNo/filterExpr/orderBy` | 走 `app/query` + `gochen-runtime/api/rest` 解析入口 |
| 读路径绕开 scoped helper | repo 有 data scope 但直接用底层 ORM 写 preload/join | 走 `ScopedQuery(...)`、`GetWith(...)`、`FindOneWith(...)` |
| 授权停在 middleware | allow 通过后 Application/Repo 写路径没有显式边界 | 走 `security.Profile.Build` → `WriteConstraint` → 显式 scoped Repo |
| 上下文语义各层重复 | HTTP、消息、repo 各自一套 tenant/operator/principal | tenant/operator/user/session 归 `contextx`，动作/范围/治理契约归对应 `auth/*` 能力包 |
| 事件链路自造 | 自写 EventBus、Outbox、投影调度、payload 升级 | 走 `eventing/*` + `messaging/*` + `eventing/upcast` |
| operation 与 process 混用 | 所有 Command 都包 tracked operation，或用 workflow 表达一次 HTTP 写入状态 | 写入口状态归 `app/operation`；多步骤推进/补偿归 `process` |
| 裸 goroutine 常驻 | scheduler、补偿任务、同步任务自己 `go func` 且无停止/恢复/失败记录 | 走 `process/task.TaskSupervisor` + `policy` |
| 可观测/缓存再封一套 | 项目内自定义 logger/metrics/cache 全局单例 | 走 `observe/logging` / `cache`，组合根显式注入 |
| 影子仓储绕隔离 | 同一张表再建一个不带 `WithIsolation` 的仓储，或注入 `*sql.DB` 手写跨租户 SQL | 走 `WithCrossIsolationReads` + `contextx.WithCrossIsolation` 正门（只读、必留痕） |
| 判定逻辑各写一遍 | 每个模块用 `EvaluatorFunc` / `ActionCheckerFunc` 现场手写 RBAC 与范围判定 | 先用 `authz.NewActionChecker()` / `authz.NewEvaluator()`，确有特殊策略再整体替换 |

## 6. 评审清单

评审下游项目时重点看下面这些：

- **路径选择**：是否按业务复杂度选了 CRUD / audited / event sourced，还是在底层自己攒？
- **组合根**：基础设施是否都在组合根装配，有没有偷偷 `new` 的依赖？
- **查询协议**：列表接口是否统一走 `app/query`？允许的过滤/排序/字段是否通过 `QuerySchema` 或 `query` tag 显式化?
- **scoped repo**：是否声明了 `WithIsolation(...)` / `WithScope(...)` / `WithSoftDeleteColumns(...)`？扩展读取是否走 scoped helper?
- **写入边界**：启用授权的写路径是否真正落到 `WriteConstraint`，而不是只在上层做布尔判断？
- **跨隔离读**：有没有"同一张表两个仓储、其中一个不声明 `WithIsolation`"或裸 SQL 跨租户查询？若确有平台级读取需求，是否走了 `WithCrossIsolationReads` + `contextx.WithCrossIsolation`，留痕出口是否真的接到了可查询的地方？
- **判定实现**：`IEvaluator` / `IActionChecker` 是自己手写的还是用了 `host/authz` 默认实现？自写的话，列表读有没有伪造占位资源来满足 `IsAllowed`（"空 allow 通行证"）？
- **上下文统一**：tenant / operator / user / session 是否统一走 `contextx`，动作、DataScope、WriteConstraint 与治理是否分别复用 `auth/action`、`auth/scoped`、`auth/governance`？
- **事件链路**：事件驱动场景是否用了 `eventing` / `messaging` 的标准能力，payload 消费是否走 `eventing/upcast`，Projection checkpoint 是否有原子边界，Outbox claim/lease 是否复用框架实现？
- **写操作可见性**：写后读模型延迟可见时是否用 `app/operation` 暴露状态；普通同步写是否避免无意义升级为 tracked？
- **后台任务**：长驻任务是否由 `task.TaskSupervisor` 管理停止、panic 和失败记录；重试/限流/熔断是否复用 `policy`？
- **可观测与缓存**：日志/指标/追踪是否由组合根通过 `observe/logging` 注入；缓存是否复用 `cache` 而不是业务仓平行抽象？
- **偏离说明**：对没走标准路径的部分，是否有明确的理由、边界和维护责任记录？

## 7. 配套阅读

- [../../README.md](../../README.md) — 项目定位与采用路径
- [../architecture/framework-design.md](../architecture/framework-design.md) — 整体架构与模块边界
- [../architecture/rbac-to-layered-authz.md](../architecture/rbac-to-layered-authz.md) — 分层授权的演化与设计思路
- 模块 README（Core）：[`app`](../../app/README.md)、[`app/operation`](../../app/operation/README.md)、[`db/orm`](../../db/orm/README.md)、[`eventing/projection`](../../eventing/projection/README.md)、[`eventing/outbox`](../../eventing/outbox/README.md)、[`messaging`](../../messaging/README.md)、[`process`](../../process/README.md)、[`policy`](../../policy/README.md)
- 模块 README（Runtime）：`gochen-runtime` 仓库 `host/README.md`、`api/rest/README.md`
- 可运行示例：[../../examples/README.md](../../examples/README.md)
