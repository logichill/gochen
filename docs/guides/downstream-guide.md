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
| 授权与错误 | `auth`、`domain/access`、`auth/http`、`auth/sqlstore`、`errors` | 身份/授权运行时模型、策略/观测、HTTP/SQL 桥接、错误码体系 |
| 后台与观测 | `task`、`observe`、`logging` | 后台 goroutine 监督、日志/指标/追踪依赖聚合 |
| 运行时基础 | `contextx`、`validate`、`clock`、`config`、`codec`、`ident`、`cache` | 上下文语义、校验、时间、配置、编解码、ID 策略、泛型缓存 |

留在业务项目的能力：具体业务实体与规则、业务专属 adapter 与外部集成、业务流程编排、组织/资源级策略判定、项目特有 UI/API 契约。

## 2. 先选对路径

按业务复杂度从上到下挑。没拿准的情况下选更简单那档，以后真需要再往下升。

| 场景 | 起点 |
| --- | --- |
| 简单管理域 | `domain/crud` + `app/crud` + `api/rest` |
| 审计型管理域（操作人、软删、恢复） | `domain/audited` + `app/audited` + `api/rest` |
| 关键交易域 / 事件驱动域 | `domain/eventsourced` + `app/eventsourced` + `eventing/*` + `messaging/*` |
| 以上任一 + 租户/授权边界 | 叠加 `auth`、`domain/access` + `db/orm/repo` 的 scoped 能力 |
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
		PermissionDefinitions(InventoryPermissions()...).
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
		host.WithName("erp"),
		host.WithHTTPServer(httpServer),
		host.WithEventBus(eventBus),
		host.WithProjectionManager(projectionManager),
		host.WithBasePath("/api/v1"),
		host.WithModuleHTTP("inventory", host.ModuleHTTPConfig{Prefix: "/inventory"}),
		host.WithModules(
			iam.NewModule,
			inventory.NewInventoryModule,
			order.NewModule,
		),
		host.WithFailFastOnRouteConflicts(true),
	); err != nil {
		log.Fatal(err)
	}
}
```

这段骨架表达三条约束：

- `main` / bootstrap 是基础设施唯一创建点；DB、HTTP server、event bus、projection manager、transport、ID 生成器都在这里确定，并按能力选择 Host option、provider 构造函数或 repo/application option 注入。
- 业务模块通过 `Provide(...)`、`RouteRegistrar(...)`、`PermissionDefinitions(...)` 声明能力，不直接构造 Host 底层 descriptor / registration / role。
- 普通业务模块优先通过构造函数拿依赖；只有编写高级模块集成、adapter 或框架扩展时，才直接使用 `host/module/runtimecap` 的 typed accessor 读取 HTTP、authz、event bus、projection 等运行时能力。

上面只展示启动骨架；真实项目里的 DB、validator、ID generator 等业务基础设施通常通过 provider 构造函数、DI 注册或 repo/application option 注入。HTTP server、transport、event bus、projection manager 等 Host 运行时能力才走对应 `host.WithXxx(...)` option。

完整可运行示例见 [`examples/host/module_boot`](../../examples/host/module_boot/main.go)。

## 4. 几条不能绕的主线

下面这些点在接入时容易走偏，事后补代价很大，值得在一开始就对齐。

### 组合根是依赖的唯一真相源

DB、HTTP server、message transport、event store、validator、ID 生成器——这些基础设施依赖必须在组合根显式创建，通过接口注入下游。handler / service / repo 内部**不要 `new` 基础设施**。

生命周期走 `host.Run(...)` / `host.New(...)`；模块声明用 `host.Module(...)` 收敛 provider、路由、事件处理器、投影注册。Host 运行时能力通过对应 `host.WithXxx(...)` 从组合根注入，业务基础设施通过 provider 构造函数或 repo/application option 注入；普通业务模块不要直接接触 descriptor、registration、role，`host/module/runtimecap` 只留给高级模块集成、adapter 或框架扩展。

### 标准 CRUD 走 builder，不要手写一整套 HTTP 样板

路由注册、分页过滤、统一响应这些东西 `api/rest` 已经做完了。只有接口明显超出标准 CRUD 语义时才建议手写 handler；即便手写，查询协议也继续复用 `rest.ParsePaginationOptions(...)` 之类的入口，不要在项目内部长出第二套 `page/size/filter/sort` 规则。

### CRUD 扩展统一走 `Hooks`

标准 CRUD 的写入扩展逻辑通过 `app/crud.Hooks` 注册，通常在组合根、路由装配或模块 builder 里用 `app.SetHooks(...)`、`rest.WithHooks(...)` 或 builder `Hooks(...)` 注入。不要通过嵌入 `Application` 后定义 `BeforeCreate` / `AfterCreate` 等方法来扩展 CRUD；这些方法不再是扩展点。

Hook 阶段由框架固定：`Before*` 在写入前执行，失败阻断写入；`After*` 在写入后、提交前执行，失败回滚；`PostCommit*` 在提交后执行，失败不回滚。需要事务后副作用（通知、外部同步、异步任务触发）优先放 `PostCommit*`。

### 查询协议统一走 `db/query`

列表、过滤、排序、字段投影都建立在 `db/query` 上。优先顺序：`query` tag 自动推导 → 显式 `QuerySchema` → 自定义 handler 复用同一解析入口。目标是让 API、repo、测试共享同一份 schema 语义，而不是同一个项目里并存"v1 / v2 / 某模块私有"三套协议。

示例：[`examples/domain/query/inferred`](../../examples/domain/query/inferred/main.go)、[`examples/domain/query/filter`](../../examples/domain/query/filter/main.go)。


### Auth 包路径约定

安全运行时主入口统一为 `gochen/auth`：Principal、Authorize、registry、policy snapshot、eval context、authz log 与 metrics 都从这里进入。repo/app 层消费的 DataScope、WriteConstraint 位于 `gochen/domain/access`，`AuthzDecision -> WriteConstraint` 投影由 `gochen/auth` 提供；HTTP 与 SQL 持久化桥接分别位于 `auth/http`、`auth/sqlstore`；CRUD tenant 策略由组合根通过 `app/crud` 显式注入。

### 授权边界收口到统一链路

tenant / 资源授权 / 写入保护**不要在 handler、service、repo 各自长一套**。标准链路是：

1. 中间件把主体写入上下文 →
2. `auth.Principal` + `domain/access.DataScope` 表达"当前是谁、默认能看哪些数据" →
3. `api/rest.WithAuthorization(...)` 在标准 CRUD 路由做动作级授权 →
4. allow 决策通过 `auth.WriteConstraintFromDecision` 投影成 `domain/access.WriteConstraint`，由 repo 写路径**显式消费** →
5. repo 扩展读取用 `ScopedQuery(...)`、`GetWith(...)`、`FindOneWith(...)`，不要直接绕回底层 ORM

两条硬边界：

- **读边界**靠 `DataScope` / scoped query，repo 不从原始 context 猜数据范围
- **写边界**靠 `WriteConstraint`，allow 之后不得退回普通 `Create/Update/Delete`

如果业务仓要让 `app/crud` 的 tenant wrapper（如 `TenantAwareWrapper` / `TenantQueryWrapper`）复用 `domain/access` 的 data scope，只能读 `DataScope.TenantIDs`，不要退回 `Principal.HomeTenantID` 这种隐式派生值。

设计背景见 [`architecture/rbac-to-layered-authz.md`](../architecture/rbac-to-layered-authz.md)。

### 事件/消息/Outbox/Projection 不要各造轮子

进入事件驱动场景就直接用 `eventing/store`、`eventing/outbox`、`eventing/projection`、`messaging`。如果项目已经开始写"事件表 + 异步补偿 + 自定义发布器 + 读模型同步器"，通常是误判了 gochen 的覆盖范围，不是 gochen 不够。

事件链路有几条硬约束：

- 消费边界统一走 `eventing/upcast.HydrateEventPayload(...)` / `DecodeEventPayload[T](...)` 做 payload 升级与强类型 hydration，不要在投影、handler、repo 里手写 `PayloadValue -> UpgradeEventData -> DeserializeFromMap`。
- 启用 Projection checkpoint 时必须配置 `IEventStreamStore`；投影处理与 checkpoint 保存要处于同一原子边界，普通投影用 `projection.NewCheckpointingProjector(...)` 包装。
- 在线处理、checkpoint 追赶、显式恢复、重建由 `ProjectionManager` 的 per-projection runtime 串行化；业务不要再给同一投影自建并发调度。
- Outbox SQL claim/mark 依赖 claim token + lease；发布器、并行发布器和 SQL repository 都应使用框架 normalize 后的配置，不要只靠 `status=pending` 自己抢任务。

示例：[`examples/infra/outbox/sql`](../../examples/infra/outbox/sql/main.go)、[`examples/infra/projection/sql_checkpoint`](../../examples/infra/projection/sql_checkpoint/main.go)、[`examples/infra/upcaster/basic`](../../examples/infra/upcaster/basic/main.go)。

### 写操作状态用 `app/operation`，过程推进才用 `process`

如果一次写操作返回成功后，读模型还没收敛，或者前端/上游需要看到 `accepted`、`processing`、`settled`、`failed` 等状态，用 `app/operation` 包装 application service 写入口。它解决的是"这次写操作现在到哪了"，不是新的业务命令分发框架。

`process/saga` / `process/workflow` / `process/lock` 只在确实存在跨步骤补偿、状态机推进或按业务 key 串行化时使用。不要因为用了 CommandBus、CQRS 或事件溯源，就默认把所有写操作升级成 tracked operation。

使用 `process/workflow.AdvanceNodeTo` 表达选择分支时，只会走被选中的直接后继；下游汇聚点默认仍按 join-all 等待全部入边。若实际语义是“任一分支到达即可继续”的 exclusive merge，请把目标节点声明为 `NodeKindTask`，不要误用默认多入边推断导致实例长期 pending。

### 后台任务用 `task` 管起来

长驻循环、定时补偿、读模型修复、外部系统同步等后台逻辑不要裸起 goroutine。统一用 `task.TaskSupervisor` 接管生命周期、panic 恢复、失败记录和停止等待；重试/限流/熔断分别接 `policy/retry`、`policy/ratelimit`、`policy/circuit`。

### 通用运行时语义统一入口

不要在 HTTP 层、消息层、repo 层各自定义一套 tenant / operator / user / session / principal 语义。核心安全运行时语义统一从 `auth` 进入，repo/app 层数据边界从 `domain/access` 进入；trace / request / tx scope / metadata propagation 统一放在 `contextx`；错误码统一走 `errors`；ID 生成走 `ident`；配置走 `config`；校验走 `validate`；缓存优先用 `cache`；可观测依赖用 `observe` / `logging` 显式注入，不要再封一层全局单例。

## 5. 最常见的偏离方式

| 反模式 | 典型表现 | 推荐做法 |
| --- | --- | --- |
| 生命周期装配散落 | 在 `main`、handler、service 里各自创建 DB、router、transport | 收口到 `host` / 组合根 |
| 手写第二套 CRUD 框架 | 标准增删改查也重写路由注册、分页协议、统一响应 | 走 `app/crud`、`app/audited`、`api/rest` |
| 查询协议碎片化 | 各模块自定义 `pageNo/filterExpr/orderBy` | 走 `db/query` + `api/rest` 解析入口 |
| 读路径绕开 scoped helper | repo 有 data scope 但直接用底层 ORM 写 preload/join | 走 `ScopedQuery(...)`、`GetWith(...)`、`FindOneWith(...)` |
| 授权停在 middleware | allow 通过后 service/repo 写路径没有显式边界 | 走 `WithAuthorization(...)` → `WriteConstraint` → repo |
| 上下文语义各层重复 | HTTP、消息、repo 各自一套 tenant/operator/principal | 核心安全语义归 `auth`，repo/app 层数据边界归 `domain/access`，链路传播归 `contextx` |
| 事件链路自造 | 自写 EventBus、Outbox、投影调度、payload 升级 | 走 `eventing/*` + `messaging/*` + `eventing/upcast` |
| operation 与 process 混用 | 所有 Command 都包 tracked operation，或用 workflow 表达一次 HTTP 写入状态 | 写入口状态归 `app/operation`；多步骤推进/补偿归 `process` |
| 裸 goroutine 常驻 | scheduler、补偿任务、同步任务自己 `go func` 且无停止/恢复/失败记录 | 走 `task.TaskSupervisor` + `policy` |
| 可观测/缓存再封一套 | 项目内自定义 logger/metrics/cache 全局单例 | 走 `observe` / `logging` / `cache`，组合根显式注入 |

## 6. 评审清单

评审下游项目时重点看下面这些：

- **路径选择**：是否按业务复杂度选了 CRUD / audited / event sourced，还是在底层自己攒？
- **组合根**：基础设施是否都在组合根装配，有没有偷偷 `new` 的依赖？
- **查询协议**：列表接口是否统一走 `db/query`？允许的过滤/排序/字段是否通过 `QuerySchema` 或 `query` tag 显式化?
- **scoped repo**：是否声明了 `WithAccessColumns(...)` / `WithSoftDeleteColumns(...)`？扩展读取是否走 scoped helper?
- **写入边界**：启用授权的写路径是否真正落到 `WriteConstraint`，而不是只在上层做布尔判断？
- **上下文统一**：tenant / operator / user / session / principal 是否统一走 `auth`，DataScope/WriteConstraint 是否统一走 `domain/access`；trace / request / tx scope 是否继续走 `contextx`？
- **事件链路**：事件驱动场景是否用了 `eventing` / `messaging` 的标准能力，payload 消费是否走 `eventing/upcast`，Projection checkpoint 是否有原子边界，Outbox claim/lease 是否复用框架实现？
- **写操作可见性**：写后读模型延迟可见时是否用 `app/operation` 暴露状态；普通同步写是否避免无意义升级为 tracked？
- **后台任务**：长驻任务是否由 `task.TaskSupervisor` 管理停止、panic 和失败记录；重试/限流/熔断是否复用 `policy`？
- **可观测与缓存**：日志/指标/追踪是否由组合根通过 `observe` / `logging` 注入；缓存是否复用 `cache` 而不是业务仓平行抽象？
- **偏离说明**：对没走标准路径的部分，是否有明确的理由、边界和维护责任记录？

## 7. 配套阅读

- [../../README.md](../../README.md) — 项目定位与采用路径
- [../architecture/framework-design.md](../architecture/framework-design.md) — 整体架构与模块边界
- [../architecture/rbac-to-layered-authz.md](../architecture/rbac-to-layered-authz.md) — 分层授权的演化与设计思路
- 模块 README：[`app`](../../app/README.md)、[`app/operation`](../../app/operation/README.md)、[`host`](../../host/README.md)、[`api/rest`](../../api/rest/README.md)、[`db/orm`](../../db/orm/README.md)、[`eventing/projection`](../../eventing/projection/README.md)、[`eventing/outbox`](../../eventing/outbox/README.md)、[`messaging`](../../messaging/README.md)、[`process`](../../process/README.md)、[`policy`](../../policy/README.md)
- 可运行示例：[../../examples/README.md](../../examples/README.md)
