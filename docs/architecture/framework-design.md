# Gochen 框架设计

本文档介绍 gochen 根模块的整体设计、核心组件和推荐用法。路径均以仓库根目录为准，如 `domain/entity.go`、`eventing/store`、`messaging/transport/memory`。

## 目录

- [1. 总体架构](#1-总体架构)
- [2. 领域层（domain）](#2-领域层domain)
- [3. 事件层（eventing）](#3-事件层eventing)
- [4. 消息系统（messaging）](#4-消息系统messaging)
- [5. 应用层与 HTTP 抽象](#5-应用层与-http-抽象)
- [6. 数据库抽象与 SQL Builder](#6-数据库抽象与-sql-builder)
- [7. Saga 与通用策略](#7-saga-与通用策略)
- [8. 错误与日志](#8-错误与日志)
- [9. 编解码机制（codec）](#9-编解码机制codec)

---

## 1. 总体架构

### 1.1 设计理念

gochen 采用 **DDD + Event Sourcing + CQRS + 消息驱动** 的组合模式，核心原则：

- **模块明确**：`domain` 专注领域模型，`eventing` 专注事件与溯源，`messaging` 专注消息管道；
- **事件为一等公民**：所有状态变化可通过事件流重建或审计；
- **读写分离**：写端优先事件溯源，读端通过投影/视图优化；
- **框架无侵入**：不绑定具体 Web 框架或 ORM，HTTP/数据库通过 `httpx` 和 `db` 抽象；
- **并发安全**：关键组件（消息总线、事件存储、投影管理器）显式考虑并发。

### 1.2 接口优先与组合根装配

gochen 是**开放式框架**，不是隐式魔法框架。所有跨包基础能力（日志、事件存储、Outbox、消息总线、验证等）都遵循三条约束：

- **先以接口形式提供**：`ILogger`、`IEventStore`、`ITransport` 等稳定接口先于任何具体实现；
- **所有依赖面向接口**：跨包字段、入参、返回值依赖接口而不是具体 struct；具体 struct（`StdLogger`、`MemoryTransport`、`SQLEventStore`）视为实现细节；
- **具体实现由组合根装配**：模块可提供默认实现，但不在库代码中硬编码选择；由业务仓库的 `main` / `cmd` 或 `host/module.Host` 的调用方在装配阶段 new 出实现、以接口形式注入。

### 1.3 生命周期命名约定

框架层生命周期入口按组件语义分三类，新增代码必须优先匹配下列口径：

| 场景 | 命名 | 约定 |
| --- | --- | --- |
| 后台服务、可启动/停止组件 | `Start(ctx)` / `Stop(ctx)` | `Start` 非阻塞启动；`Stop` 使用 ctx 控制等待上界并收尾后台 goroutine。 |
| 阻塞式主循环或进程级 runtime | `Run(ctx)` / `Shutdown(ctx)` | `Run` 持有主循环；`Shutdown` 从外部请求终止并释放运行时资源。 |
| 一次性资源、I/O handle | `Open` / `Close` 或仅 `Close` | 与标准库资源语义一致，例如 DB、Rows、Tx、文件句柄、缓存资源包装。 |

当前约定：

- `messaging.ITransport`、`host/capability.ITransport`、`TaskSupervisor`、`IdempotencyMiddleware`、Outbox Publisher 均使用 `Stop(ctx)` 作为停止入口，不暴露 `Close()` 作为后台服务主入口。
- `messaging.ITransportStopSnapshot` 是可选能力，用于停止时额外返回队列中未处理消息快照；统一停止入口应优先使用 `messaging.StopTransport(ctx, transport)`，由它处理 `StopWithSnapshot(ctx)`、`Stop(ctx)` 与“已停止”幂等语义。
- `clock.Timer/Ticker`、`observe.Timer` 的 `Stop()` 沿用标准库计时器语义，不属于服务生命周期。
- `db/sql/stdsql.DB`、`Rows`、`Tx` 与 `eventing/store/cached.CachedEventStore` 是资源型对象，继续使用 `Close()`。

### 1.4 并发原语使用约定

框架层新增共享状态时，先按状态语义选择并发原语，而不是为了追求“无锁”替换已有互斥锁：

| 原语 | 适用场景 | 约束 |
| --- | --- | --- |
| `atomic.Value` | 读多写少、单槽位热替换的配置、recorder、metrics 指针 | 首次 `Store` 后动态类型必须固定；不得 `Store(nil)`；不承载多个字段的不变量。 |
| `sync.Mutex` | 多字段不变量、生命周期状态迁移、channel close 保护、一次发布/停止流程串行化 | 涉及多个字段一致性时优先使用；锁内只做必要状态变更，避免持锁执行外部回调。 |
| `sync.RWMutex` | registry、cache、store 等读多写少的 map/slice 状态 | 读锁内不得向外暴露可变内部 map/slice；需要返回集合时复制快照。 |

新增并发字段时应在字段附近注明保护对象和调用并发语义，例如 `mu protects started/stopped/ch` 或 `recorder holds a single metrics recorder pointer`。若状态更新需要同时观察或修改多个字段，优先使用 `sync.Mutex` 保持不变量清晰，不用 `atomic.Value` 或多组 atomic 拼装一致性。

### 1.5 测试分层与 integration tag

默认 `go test ./...` 只承载单元测试、纯内存测试、`httptest`、`t.TempDir()` 临时文件测试，以及 `modernc.org/sqlite` 的内存或临时文件 SQLite 测试；这些测试不得依赖开发机固定路径或已启动的外部服务。

凡是依赖真实外部资源的测试，必须加 `//go:build integration` 并从默认测试集中隔离，包括：

- 真实 MySQL、Postgres、Redis、消息队列、对象存储或第三方网络服务；
- Docker、testcontainers、外部进程或需要固定端口监听的服务；
- 开发机固定文件路径、共享目录或需要人工预置的环境状态。

集成测试统一入口为 `go test -tags=integration ./...` 或 `make integration`。新增 integration 测试时，应在测试文件头部说明必需环境变量、默认资源名和清理策略，避免 CI 或本地默认测试误触外部资源。

### 1.6 目录速览

```text
domain/                   # 领域层抽象（不依赖基础设施）
  crud/                   # CRUD 端口抽象
  audited/                # 审计端口抽象
  eventsourced/           # 事件溯源端口与模型

eventing/                 # 事件基础设施（不依赖 domain）
  event.go                # 事件模型
  store/                  # 事件存储（内存/SQL/缓存/snapshot）
  projection/             # 投影与 Checkpoint
  outbox/                 # Outbox 模式
  bus/                    # 事件总线（基于 messaging.MessageBus）
  registry/               # 事件类型注册表
  upcast/                 # 事件升级链
  monitoring/             # 监控指标

messaging/                # 消息总线与传输
  bus.go                  # MessageBus + 中间件
  transport/              # 传输实现（direct/memory）
  deadletter/             # 死信记录抽象与 provider
  command/                # 命令模型与命令总线

app/                      # 应用服务层
  crud/ audited/ eventsourced/
  access/                 # 写入约束、资源边界契约
  operation/              # 写操作协议包装

db/                       # 数据访问
  dialect/                # 方言抽象
  lock/                   # DB 锁
  orm/                    # ORM 适配器抽象（IOrm/IModel/IOrmSession）
  query/                  # 查询协议（IQueryableRepository）
  sql/sqlbuilder/         # SQL Builder

httpx/                    # HTTP 抽象
  nethttp/                # 基于 net/http 的默认实现
  middleware/             # 通用中间件

auth/                     # 身份与分层授权运行时（http/sqlstore）
domain/access             # repo/app 层授权边界 contract（DataScope/WriteConstraint）
host/ di/                 # 生命周期、模块装配、依赖注入
contextx/ logging/ errors/ validate/ clock/ config/ codec/ ident/
process/ policy/ task/    # Saga、Workflow、重试、限流、熔断、任务监督
api/rest/              # REST CRUD 构建器
```

---

## 2. 领域层（domain）

领域层承载业务语义与模型边界，尽量对基础设施和具体框架无感知——不直接依赖 HTTP/数据库/事件总线实现，而是通过抽象接口（`errors`、`logging`、`validate`、`eventing` 抽象）协作。

### 2.1 实体与聚合根

基础接口在 `domain/entity.go`，事件溯源聚合根在 `domain/eventsourced/entity.go`：

- `IEntity[ID]` — 带 ID 与版本的实体
- `IDomainEvent` — 领域事件接口，提供 `EventType()`
- `IEventSourcedAggregate[ID]` — 事件溯源聚合根接口

事件溯源聚合根实现 `EventSourcedAggregate[T comparable]`：

```go
type IEventSourcedAggregate[T comparable] interface {
    IEntity[T]
    GetAggregateType() string
    GetExpectedVersion() uint64
    ApplyEvent(evt domain.IDomainEvent) error
    GetUncommittedEvents() []domain.IDomainEvent
    MarkEventsAsCommitted()
}
```

典型使用：

```go
type BankAccount struct {
    *eventsourced.EventSourcedAggregate[int64]
    Balance int
}

func NewBankAccount(registry *eventsourced.MetadataRegistry, id int64) (*BankAccount, error) {
    a := &BankAccount{}
    agg, err := eventsourced.InitAggregate[int64](registry, a, id, "BankAccount")
    if err != nil { return nil, err }
    a.EventSourcedAggregate = agg
    return a, nil
}
```

聚合本身不做内部加锁，由应用服务在命令处理层保证对单个聚合实例的串行访问。

模块集中装配可用 `eventsourced.Aggregate(...)` 声明单项注册、`EventSourcedSupport` 复用样板、`RegisterModuleAggregates(registry, ...)` 在启动期统一注册并校验 metadata。

### 2.2 仓储接口

`domain/crud` 提供基础仓储 ports，`app/crud` 额外提供应用层事务运行协议：

- `IRepository[T, ID]` — 基础 CRUD（Get/Create/Update/Delete/List/Count/Exists）
- `IBatchOperations[T, ID]` — 批量操作

通用查询定义在 `db/query`，主路径消费与 adapter 输入协议明确分层：

- `QueryRequest` / `QueryOptions` — application/repository 主路径消费的统一查询模型
- `QueryFilters` — 按字段归组的 map-like 过滤表达式集合
- `Filter` / `FilterOp` — HTTP/CLI 等 adapter 输入协议
- `IQueryableRepository[T, ID]` — 在 `IRepository` 基础上补 `Query/QueryOne/QueryCount`
- `app/crud.ITransactional` — 应用层事务运行接口（`WithinTx(ctx, fn)`）

### 2.3 事件溯源服务

事件溯源能力分两层：

**领域层（`domain/eventsourced`）**：定义接口与核心协议，不依赖基础设施

- `IEventSourcedAggregate[ID]`、`EventSourcedAggregate[T]`
- `Metadata` / `ScanMetadata` / `MetadataRegistry` — 装配期预编译的事件 handler 元数据与运行时 registry
- `IEventSourcedRepository[T, ID]` — 仓储接口（Save/Get/Exists/GetAggregateVersion）
- `IDomainEventStore[ID]` — 领域事件存储端口

**应用层（`app/eventsourced`）**：提供具体实现与扩展能力，依赖 eventing 基础设施

- `DomainEventStore` — 桥接 `eventing/store` + Snapshot + Outbox + EventBus
- `EventSourcedRepository[T, ID]` — 默认仓储实现
- `EventSourcedService[T, ID]` — 命令执行模板（加载聚合 → 执行 handler → 保存聚合），可选并发重试
- `SnapshottingRepository[T, ID]` — 叠加快照恢复/保存策略
- `History` — 事件历史查询
- `AsCommandMessageHandler` — 将 `EventSourcedService` 适配为 `messaging.IMessageHandler`

### 2.4 值对象（VO）约定

gochen 不在框架层提供 `IValueObject` 或 VO 基类，VO 由业务域自行定义。推荐约定：

- **强类型**：优先自定义类型替代裸 `string/int64`（`type Email string`、`type TenantID string`）
- **构造即校验**：提供 `NewXxx(...) (Xxx, error)` 或 `ParseXxx(string)`
- **不可变**：struct VO 使用未导出字段 + 只读方法
- **判等语义**：非可比较/有归一化的 VO 提供 `Equal(other) bool`
- **序列化稳定**：对字符串型 VO 推荐实现 `encoding.TextMarshaler/TextUnmarshaler`
- **可选**：希望进入通用校验链路时实现 `domain.IValidatable`（`Validate() error`）

---

## 3. 事件层（eventing）

### 层间口径

- `messaging` — **消息机制内核**：message envelope、总线、传输、中间件、DLQ、命令语义
- `eventing` — **事件事实链路**：事件模型、事件存储、Outbox、投影、订阅、监控
- `eventing` 可以复用 `messaging` 做投递，但 `messaging` 不反向依赖 `eventing`

### 3.1 事件模型

```go
// IEvent：用于总线/投影等场景的"ID 无关"事件视图
type IEvent interface {
    messaging.IMessage
    GetAggregateType() string
    GetVersion() uint64
}

// ITypedEvent[ID]：带强类型聚合 ID 的事件接口
type ITypedEvent[ID comparable] interface {
    IEvent
    GetAggregateID() ID
}

// IStorableEvent[ID]：用于事件持久化的扩展接口
type IStorableEvent[ID comparable] interface {
    ITypedEvent[ID]
    GetSchemaVersion() int
    SetAggregateType(string)
    Validate() error
}

type Event[ID comparable] struct {
    messaging.Message
    AggregateID   ID
    AggregateType string
    Version       uint64
    SchemaVersion int
}
```

`Event[ID]` 同时实现传输与存储接口，支持 SchemaVersion 与 Metadata。设计选择是显式的：**event 是 message 的特化，而非另一套平行信封协议**。事件层复用消息层的 envelope 与链路传播，但聚合版本、事件存储、投影与重放仍留在 `eventing`。

### 3.2 事件存储接口

核心接口（`eventing/store/eventstore.go`）：

```go
type IEventStore[ID comparable] interface {
    AppendEvents(ctx context.Context, aggregateID ID, events []eventing.IStorableEvent[ID], expectedVersion uint64) error
    LoadEvents(ctx context.Context, aggregateID ID, afterVersion uint64) ([]eventing.Event[ID], error)
    LoadEventsByType(ctx context.Context, aggregateType string, aggregateID ID, afterVersion uint64) ([]eventing.Event[ID], error)
    HasAggregate(ctx context.Context, aggregateID ID) (bool, error)
    GetAggregateVersion(ctx context.Context, aggregateID ID) (uint64, error)
}
```

扩展接口 `IEventStreamStore[ID]`：全局事件流（游标分页）+ 聚合顺序流（按版本片段读取），用于投影回放、历史导出等流式读取场景。

内置实现包括 `MemoryEventStore`、`sqlstore.SQLEventStore`、`cached.CachedEventStore`，默认为 `ID=int64`。

**聚合 ID 类型策略**：

- **默认 `int64`**（雪花 ID 或自增）：绝大多数关系型数据库场景，与现有 SQL schema、索引、`ident/snowflake` 组合良好
- **`string` / UUID**：业务主键天然是字符串或需要跨系统对齐时，使用 `sqlstore.NewSQLEventStoreWithCodec[string](db, "event_store", codec)` 显式构造，并通过 `idcodec.NewString[...]()` 提供 codec；表结构中 `aggregate_id` 列从 `INTEGER` 调整为 `TEXT`
- **自定义强类型 ID**：在事件载荷与 EventStore 实现中统一使用，要求支持 `comparable`，序列化时稳定映射

单个 bounded context 内保持统一策略，减少跨上下文迁移复杂度。SQL schema 细节见 [`../guides/db-schema-migration-guide.md`](../guides/db-schema-migration-guide.md)；事件 schema 演进、upcast、滚动升级纪律见 [`../reference/event-schema-evolution.md`](../reference/event-schema-evolution.md)。

### 3.3 投影管理

`eventing/projection` 提供投影接口与管理器：

- `IProjection` — 投影最小接口
- `ProjectionManager` — 注册、状态、检查点与事件处理，通过 `eventing/bus.IEventBus` 订阅事件
- `ICheckpointStore` — 检查点接口，可选 SQL 实现

live-only 模式下，handler 串行调用同一 projection 的 `Handle`，成功递增 `ProcessedEvents`，失败递增 `FailedEvents` 并记录 `LastError` + DeadLetter 钩子。checkpoint 模式下，handler 仅在 projection 为 running 时唤醒 projection runtime 从 EventStore 按 checkpoint 游标追赶，并优先使用 runtime 内存 cursor 跨越尚未持久化的批量 checkpoint 窗口；显式恢复在运行期按 runtime/durable 较新游标继续，避免异步 transport 乱序投递或旧持久 checkpoint 导致重复推进。

**并发与线程安全**：

- `ProjectionManager` 以 per-projection runtime 管理状态、handler、checkpoint tracker 与内存 cursor；全局锁只保护 runtime 注册表，runtime 自己保护状态快照。
- 同一 projection 的在线处理、checkpoint 追赶、`ResumeFromCheckpoint` 与 `RebuildProjection` 串行执行；不同 projection 之间可并发。
- checkpoint 模式以 EventStore 的全局流顺序为准，启用 checkpoint store 时必须配置 `IEventStreamStore`；未启用 checkpoint 的 live-only 模式只保证同一 projection 不重入，不承诺崩溃恢复。
- `RebuildProjection` 期间状态标记为 `rebuilding`，在线处理会等待同一 runtime 串行边界；重建完成后回到 `stopped`，需显式 `StartProjection`

### 3.4 Outbox 与监控

- `eventing/outbox` — 基于 `db/sql/sqlbuilder` 的 SQL Outbox 仓储（`sql_repository.go`），提供入库、查询待发布、标记已发布/死信等能力
- Outbox 串行 `Publisher` 与并行 `ParallelPublisher` 共享 `outboxPublisherCore` 的 claim、decode/upcast、publish、mark、DLQ、metrics、cleanup 核心；并行入口仅额外负责 worker pool、分片分发、批量 mark 与停止 drain。
- `eventing/monitoring` — `monitoring.Registry`（指标 + 健康检查）与 `monitoring.NewHTTPHandler`（`/healthz`、`/metrics`、`/snapshot`）

核心组件埋点通过 `SetMetricsRecorder(...)` 显式注入（推荐在组合根统一将 `reg.Metrics` 注入到 EventStore/Outbox/Projection/Snapshot/Cache），避免隐式全局依赖。

---

## 4. 消息系统（messaging）

这一层只回答"消息如何被封装、路由、投递和失败收敛"，不回答"事件如何存储/重放/构建读模型"——那是 `eventing` 的事。

### 4.1 消息与处理器

- `messaging.Message` / `IMessage` — 通用消息结构（ID、Kind、Type、Timestamp、Payload、Metadata）
  - `Kind` — 消息类别（command/event/query/unknown），用于语义判定与观测
  - `Type` — 具体消息名，用作路由键/订阅键
  - `Metadata` — string-only，跨进程/跨 JSON 更稳定
- `IMessageHandler`：

```go
type IMessageHandler interface {
    Handle(ctx context.Context, message IMessage) error
    Type() string
}
```

### 4.2 MessageBus 与中间件

`messaging.MessageBus`（`messaging/bus.go`）：

- 接口 `IMessageBus`，支持 `Subscribe`（返回 `UnsubscribeFunc`）/ `Publish` / `PublishAll` / `Use`
- 内部通过 `Transport` 抽象与具体传输实现解耦
- 支持中间件链（`IMiddleware`）与处理器错误钩子 `HandlerErrorHook`（含 handler panic 统一收敛）

**并发与线程安全**：`Publish/PublishAll/Subscribe` 与 `UnsubscribeFunc` 可并发调用；`Use/SetHandlerErrorHook` 有锁保护但推荐装配期完成；`Transport` 决定同步/异步语义。

### 4.3 传输实现

- `memory` — 基于内存队列 + worker 池的异步传输
- `direct` — 同步传输（`Publish` 在当前 goroutine 内执行 handler）

传输实现是后台服务，生命周期统一为 `Start(ctx)` / `Stop(ctx)`；需要在停止时读取未处理消息快照的实现可额外实现 `messaging.ITransportStopSnapshot.StopWithSnapshot(ctx)`。

仓库不内置 NATS/Redis 等网络传输。推荐在业务仓库中基于 `messaging.ITransport` 自行封装，可参考 `messaging/transport/natsjetstream/README.md`、`messaging/transport/redisstreams/README.md`。

### 4.4 命令层

`messaging/command` 把命令作为 `Message` 的特化：`command.Command` 携带聚合路由字段（`AggregateID`、`AggregateType`），约定 `Kind=command`、`Type=commandType`。

角色拆分：

- `command.CommandBus` — 命令投递端口，对 `messaging.IMessageBus` 的薄包装，负责 `Dispatch`
- `command.CommandExecutor` — 本地命令执行器，负责 handler registry、执行侧中间件、同步 `Execute`
- `messaging/command/middleware` — 聚合锁、幂等性、验证等中间件，按需挂到投递侧或执行侧

与 `app/eventsourced.EventSourcedService` 的对接通过 `app/eventsourced.AsCommandMessageHandler`。

---

## 5. 应用层与 HTTP 抽象

应用层位于领域层之上，把领域模型与仓储组合为可复用的"应用服务模板"，并通过 HTTP 等接口对外暴露 API。应用层依赖领域抽象与 HTTP 抽象，不直接依赖具体 Web 框架或数据库实现。

### 5.1 应用服务模板

`crud.Application[T]`（`app/crud`）基于 `domain` + `domain/crud` 提供：

- `ListByQuery` — 基于统一 `QueryRequest` 的列表查询
- `ListPage` — 分页查询（支持过滤/排序/受控高级过滤）
- `CountByQuery` — 条件统计
- 批量：`CreateAll`、`UpdateAll`、`DeleteAll`

`ServiceConfig` 仅承载"确实会生效"的参数：

- `AutoValidate` — 是否执行注入的 validator
- `MaxBatchSize` — 批量写入上限
- `MaxPageSize` — 分页单页上限（与批量写入独立）

当仓储不支持 `db/query.IQueryableRepository` 时，只要 query/page 请求携带过滤/排序/高级过滤条件，就返回 `errors.Unsupported`，不静默忽略。

新代码直接导入 `app/crud`（CRUD）或 `app/audited`（审计/软删/恢复）。

### 5.2 HTTP 抽象与 API 构建

- `httpx` — HTTP 抽象（上下文、请求、响应）与中间件约定
- `httpx` 同时提供统一响应 helper：成功路径 `WriteSuccess/WriteCreated/WriteAccepted/WriteNoContent`，失败路径 `WriteError/WriteErrorCode/WriteErrorExtra`，默认 `ResponseMessage`
- `httpx/nethttp` — 基于 `net/http` 的默认实现
- `api/rest` — 简化的 REST CRUD 构建器，将 `app/crud` / `app/audited` 的应用服务暴露为 HTTP API，并与 `errors.Normalize` 协作统一错误返回
- `api/rest.RouteConfig` 按关注点分组：`Routing` 承载路径、ID codec 与路由开关；`Query` 承载分页、白名单与查询 schema；`Body` 承载请求体大小与 API 校验；`HTTP` 承载 CORS 与路由中间件；`Response` 承载错误处理、响应包装与创建状态码；`Audit` 承载 operator 提取；`Authorization` 保持独立授权配置

上层业务项目可以直接用 `httpx/nethttp.NewServer`，也可以本地实现 `httpx.IServer` 适配 Gin/Fiber/Echo。

### 5.3 运行时上下文语义边界

- `auth` 是框架内安全相关运行时语义的核心入口；`tenant` / `operator` / `user` / `session` / `principal` / authorization / policy snapshot / eval context / authz log / metrics 的 typed API 优先定义在这里，repo/app 层 DataScope/WriteConstraint contract 位于 `domain/access`，auth 负责投影适配
- `contextx` 承担 trace / request / tx scope / metadata propagation 等链路语义，并作为 `auth` 这类上层语义入口的底层承载
- `contextx/fields` 是轻量字段读写子包，只放 `tenant_id`、`trace_id`、`request_id`、`operator` 的 key 与 context accessor；`logging` 等底层包只读字段时依赖该子包，避免传递依赖 trace id 生成器
- `contextx.IMetadata` 只是"可读写元数据载体抽象"，用于跨边界传播上下文字段，不是运行时语义的定义层
- `httpx` 只负责 HTTP 协议适配与桥接：从 header/cookie/request 提取字段写入 `auth` / `domain/access` / `contextx`，或把上下文字段回写响应；不再定义 tenant/user/session/trace 等语义本身
- `messaging.Metadata` 承载和传播上下文字段；消息系统可与 `contextx` 双向桥接，但不是运行时语义来源
- 跨边界传播原则：核心安全语义优先信 `auth`，repo/app 层数据边界优先信 `domain/access`，链路语义优先信 `contextx`，metadata 为传播副本；仅在缺失时从 metadata 补齐，不允许 metadata 覆盖已有语义
- 默认可传播字段：`tenant_id`、`trace_id`、`request_id`、`operator`；`user_id`、`session_id` 属更强主体态，默认不作为通用传播字段

### 5.4 分层授权与写入约束

`auth` 承载框架的分层授权模型，五个核心对象各司其职：

- **`Principal`** — 谁在操作，以及当前以哪个范围（`ActiveScopeID`）工作
- **`Resource`** — 被操作的对象，含资源归属字段 `ManagedScopeID`
- **`AuthzDecision`** — 授权判定结果（Allow / Deny / error → fail-closed）
- **`DataScope`** — 当前默认能看到哪些 managed scopes 下的数据（一个**范围集合**，不是 scope 等值条件）
- **`WriteConstraint`** — 由 `AuthzDecision` 投影出的显式写入约束，repo 在最终写边界强制执行

与其他层的协作：

- **HTTP 中间件**注入 `Principal`，处理登录后的 `ActivateScope` 选择；
- **`api/rest.WithAuthorization(...)`** 在标准 CRUD 路由做动作级授权，allow 决策通过 `auth.WriteConstraintFromDecision` 投影为 `domain/access.WriteConstraint`；
- **`db/orm/repo`** 基于 `DataScope` 自动注入查询边界（`managed_scope_id IN (...)`），基于 `WriteConstraint` 强制校验写边界（resource_id + managed_scope_id + revision）；
- **service / application** 编排复杂业务时显式调用 `Authorize(...)` 并把 decision 传给写层。

模型从 RBAC 出发、逐步引入 scope / binding / managed_scope_id / ActiveScope / DataScope / WriteConstraint 的完整演化叙事与设计推演见 [`rbac-to-layered-authz.md`](rbac-to-layered-authz.md)。

---

## 6. 数据库抽象与 SQL Builder

### 6.1 基础 DB 抽象（`db`）

定义统一的 `db.IDatabase` 接口，默认基于 `database/sql`（`db/sql/stdsql`），屏蔽驱动差异。

### 6.2 SQL Builder（`db/sql/sqlbuilder`）

模块化 SQL 构建层：

- `ISql` 接口支持 Select/Insert/Update/Delete/Upsert
- 每类语句拆到独立文件
- 结合 `db/dialect.Dialect` 处理 MySQL/SQLite/Postgres 的方言差异（DELETE LIMIT、Upsert 等）

事件存储、Outbox、投影检查点的 SQL 实现都基于这一层构建。

---

## 7. Saga 与通用策略

### 7.1 Saga（`process/saga`）

Saga 编排器基于命令与消息总线实现跨聚合长事务：

- 支持按步骤定义补偿
- 状态持久化：`state_store.go` + 内存实现 `state_store_memory.go`
- 配合 `messaging/command` 与 `eventing` 使用

### 7.2 其他通用能力

- `policy/retry` / `policy/ratelimit` / `policy/circuit` — 重试、限流、熔断
- `process/workflow` — 轻量的流程/状态机
- `process/lock` — 按业务 key 串行化执行（`process/lock/sql` 提供 DB 分布式实现）
- `app/operation` — 对外可观察的写操作协议包装

`policy` 与 `process` 不直接依赖具体业务模型或 HTTP/server 适配层，通过接口（`messaging/command`、`eventing`）协作。

---

## 8. 错误与日志

### 8.1 错误设计

gochen 采用"**错误码 + 结构化错误类型 + 边界层规范化**"的组合：

- 核心包 `gochen/errors`：
  - 定位为标准库 `errors` 的超集：`New`、`Is`、`As`、`AsType`、`Join`、`Unwrap`、`ErrUnsupported` 与标准库语义一致
  - 错误码枚举 `errors.ErrorCode`
  - `*errors.AppError` 承载错误码、消息、cause、结构化上下文（Details）
  - 创建：`errors.New` / `errors.NewCode` / `errors.NewCodeWithCause` / `errors.Wrap`
  - 扩展点：`errors.IErrorCoder` + `errors.Normalize` — 业务/第三方错误可保留自身类型，在边界层自动落入统一错误码；标准库 `errors.ErrUnsupported` 会归一为 `errors.Unsupported`

- 模块侧约定（**强制**）：
  - `domain` / `eventing` / `messaging` / `policy` / `app` 等模块**不再定义**模块级错误码、错误类型或哨兵
  - 统一使用 `errors.NewCode` / `NewCodeWithCause` / `Wrap` + `WithContext` / `WithDetails` 携带上下文
  - "能力缺失/不支持"的场景统一返回 `errors.Unsupported`，不自造 `ErrXxxNotSupported`

- 判断与诊断：
  - `errors.Is(err, code)` 做错误码判定
  - `errors.Code(err)` 取错误码字符串（未知默认归为 `Internal`）
  - 需要结构化上下文时：`var appErr *errors.AppError; errors.As(err, &appErr)`，推荐字段 `aggregate_id`、`event_id`、`expected_version`、`actual_version`、`table`、`operation`
- **调用栈（仅 5xx）**：`errors.NewCode/NewCodeWithCause/Wrap` 在 `HTTP status >= 500` 时捕获并写入 `details["stack"]`；`logging.StdLogger` 在含 `logging.Error(err)` 时自动追加 `stack=...`。stack 不会回传到对外 HTTP 响应。

模块层面的错误使用要点：

- **EventStore 实现**：并发冲突返回 `Concurrency`、聚合不存在返回 `NotFound`，在 `Details` 中附带 `aggregate_id`、`event_id`、`expected_version`、`actual_version`。调用方判断聚合不存在统一用 `errors.Is(err, errors.NotFound)`，不要通过 `len(events)==0` 自己推断
- **MessageBus / CommandBus**：注册 handler 失败应作为装配错误 fail-fast（`log.Fatal` 或 panic）；`Dispatch` 只表达"命令是否成功进入 transport"，不等价于 handler 完成；`CommandExecutor` 不对业务 handler 错误重试或吞噬，需要时在中间件里转成 `errors.AppError`
- **HTTP 层**：`nethttp.WriteErrorResponse` 统一通过 `errors.Normalize` 映射错误并选择 HTTP 状态码；handler 专注业务错误，异常情况交由 nethttp 响应写出函数或中间件处理；如需提前写出响应，通过 `ctx.Set("response_written", true)` 标记避免重复输出
- **应用层**：Application / DomainEventStore 在持久化或发布失败时返回结构化错误并保留底层 `cause`；Outbox 模式下 DomainEventStore 将 Outbox 仓储错误原样向上返回；`PublishEvents=true` 且 `OutboxRepo=nil` 时退化为"直接发布"（非原子），仅适合单元测试，生产环境显式配置 OutboxRepo

### 8.2 日志（`logging`）

统一的 `logging.ILogger` 接口与基础实现。所有核心组件（eventing、messaging、saga、app）通过它输出结构化日志。

默认值与注入边界：

- **推荐模式**：组件持有 `logger logging.ILogger` 字段，通过构造函数或组合根显式注入
- **不用可变全局 logger**：框架不提供 `GetLogger/SetLogger` 这类全局读写出口，避免隐式共享与并行测试污染
- **兜底**：调用方未注入时用 `logging.NewStdLogger("")` 或 `logging.ComponentLogger("component.name")` 作为最小可用默认实现（不共享全局单例）
- **上下文字段**：`logging.ContextFields` 只依赖 `contextx/fields` 读取字段，不依赖 `contextx.Background()` / `GenerateTraceID()` / `ident/snowflake`

### 8.3 监控

- `eventing/monitoring` — 事件/投影/Outbox 监控入口：
  - **写入路径**：通过 `SetMetricsRecorder(...)` 显式注入，避免隐式全局依赖
  - **读出口兜底**：`monitoring.DefaultRegistry` / `SetDefaultRegistry` 仅用于 HTTP handler 等"读取/导出"场景的默认定位，不推荐业务代码运行期通过它写指标
- 传输层（如 `messaging/transport/memory`）提供 `Stats` 方法查看队列深度、处理器数量、worker 数等运行状态

这些监控点可以集成到 Prometheus 或其他监控系统，观测 EventStore、Outbox、投影和消息总线的健康状况。

---

## 9. 编解码机制（codec）

`codec` 模块的目标不是提供某一种具体序列化方案，而是统一表达：

- 一个业务值 `T`
- 如何与某种原始载体 `Raw` 互转

### 9.1 核心接口

```go
type ICodec[T any, Raw any] interface {
    Encode(v T) (Raw, error)
    Decode(raw Raw) (T, error)
}
```

`Raw` 也是接口语义的一部分，不能被隐藏掉。如果只有 `T` 一个类型参数，不同 codec 的"原始载体"会被压扁成 `any`，抽象失真：ID codec 的 raw 来自路由参数、DB driver Scan/Bind，用 `any` 合理；JSON codec 的 raw 明确是 `[]byte`，退化成 `any` 后调用方无法从类型上看出边界。

因此 `idcodec` 实现 `ICodec[ID, any]`，`jsoncodec` 实现 `ICodec[T, []byte]`。

### 9.2 包结构与职责

- `codec` 根包 — 只放抽象，不放具体实现
- `codec/idcodec` — 处理 `int64` / `string` 及强类型别名与 `any` raw 的互转；提供 `NewInt64` / `NewString` / `NewDefault`
- `codec/jsoncodec` — 处理 `T <-> []byte(JSON)`，提供 `UseNumber`、尾随数据校验、`RawNumber` 等 JSON 专属语义

### 9.3 实践建议

- ID、DB、路由参数等 raw 值场景优先 `codec/idcodec`
- JSON 文本场景优先 `codec/jsoncodec`
- 未来出现 protobuf、msgpack 等新载体，继续复用 `ICodec[T, Raw]`，新增对应子包即可
