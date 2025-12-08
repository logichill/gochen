# Gochen 框架设计文档（根模块）

本文档介绍当前 gochen 根模块的整体设计、核心组件和推荐用法。所有路径均以本仓库根目录为准，例如 `domain/entity`、`eventing/store`、`messaging/transport/memory` 等。

## 目录

- [1. 总体架构](#1-总体架构)
- [2. 领域层（domain）](#2-领域层domain)
- [3. 事件层（eventing）](#3-事件层eventing)
- [4. 消息系统（messaging）](#4-消息系统messaging)
- [5. 应用层与 HTTP 抽象](#5-应用层与-http-抽象)
- [6. 数据库抽象与 SQL Builder](#6-数据库抽象与-sql-builder)
- [7. Saga 与通用模式](#7-saga-与通用模式)
- [8. 监控与日志](#8-监控与日志)

---

## 1. 总体架构

### 1.1 设计理念

gochen 采用 **DDD + Event Sourcing + CQRS + 消息驱动** 的组合模式，核心原则：

- **模块明确**：`domain` 专注领域模型，`eventing` 专注事件与溯源，`messaging` 专注消息管道；
- **事件为一等公民**：所有状态变化可以通过事件流重建或审计；
- **读写分离**：写端优先事件溯源，读端通过投影/视图优化；
- **框架无侵入**：不绑定具体 Web 框架或 ORM，HTTP/数据库通过 `http` 和 `data/db` 抽象；
- **并发安全**：关键组件（消息总线、事件存储、投影管理器等）在设计上显式考虑并发。

### 1.2 接口优先与组合根装配（核心约束）

本仓库在设计上遵循一条核心约束，用于指导所有基础能力与依赖关系的实现方式：

- **所有基础能力必须先以接口形式提供**  
  - 日志、数据库、事件存储、快照、Outbox、消息总线、验证等基础设施，都应先定义稳定接口（例如 `ILogger`、`IDatabase`、`IEventStore`、`ITransport`、`ISnapshotStore`），再提供具体实现。  
  - 包内部的具体 struct（如 `StdLogger`、`MemoryTransport`、`SQLEventStore`、`SQLStore`）应视为接口的实现细节，而不是跨包依赖的类型。

- **所有依赖一律面向接口，而非具体实现**  
  - 跨包协作的组件（领域服务、仓储、Server、MessageBus、Outbox、ProjectionManager 等）在字段、入参、返回值上都应依赖接口类型，而不是具体 struct。  
  - 允许在实现模块内部（同一包内）使用具体 struct 作为参数或字段，但一旦暴露为公共 API，应优先改为接口。

- **提供能力的模块可以提供默认实现，由上层组合根装配选择**  
  - 每个“提供能力”的模块（如 `eventing/store/sql`、`messaging/transport/memory`、`eventing/store/snapshot`、`logging`）可以提供一组默认实现，但不直接在库代码中“硬编码选择”。  
  - 具体选用哪种实现，应由组合根（如 examples、业务仓库的 `main`/`cmd`、或 `server.Server` 的调用方）在装配阶段显式 new 出实现，再以接口形式注入到上层服务/组件中。

- **代码审查与重构时的检查要点**  
  - 新增接口时：确保命名以 `I*` 开头，并优先在抽象包中定义（如 `logging`、`eventing/store`、`messaging`）。  
  - 新增依赖时：检查字段/入参/返回值是否可以依赖已有接口，而不是直接依赖 struct；如果没有合适接口，考虑先补接口再引入依赖。  
  - 在实现模块中引入新的 struct 时：只允许在“内部构造层 + 测试/示例”中直接使用，在跨包 API 上一律通过接口暴露。

### 1.2 目录结构概览

当前根目录核心结构：

```text
domain/                      # 领域层抽象（不依赖基础设施）
  entity.go                  # 通用实体与领域事件接口（IEntity / IDomainEvent）
  crud/                      # CRUD 场景抽象（IRepository / IService）
  audited/                   # 审计场景抽象（IAuditedEntity / IAuditedService）
  eventsourced/              # 事件溯源抽象（IEventSourcedAggregate / IEventSourcedRepository / EventSourcedService）

eventing/                    # 事件基础设施层（不依赖 domain）
  event.go                   # 事件模型（IEvent / IStorableEvent / Event）
  store/                     # 事件存储接口与实现（IEventStore / MemoryStore / SQLStore）
  projection/                # 投影管理器与 Checkpoint（ProjectionManager / ICheckpointStore）
  outbox/                    # Outbox 模式（IOutboxRepository / Publisher / ParallelPublisher）
  bus/                       # 事件总线（IEventBus，基于 messaging.MessageBus）
  monitoring/                # 监控指标（Metrics / Health）

messaging/                   # 消息总线与传输层
  message.go                 # IMessage / Message
  handler.go                 # IMessageHandler
  bus.go                     # MessageBus 实现 + 中间件
  command/                   # 命令模型与命令总线（ICommand / CommandBus）
  transport/                 # 传输实现（memory / natsjetstream / redisstreams / sync）
  bridge/                    # 远程桥接（HTTP / gRPC）

app/                         # 应用服务层（依赖 domain + eventing + data）
  application/               # CRUD 应用服务（Application 泛型服务 + 查询/分页）
  api/                       # HTTP API 构建器（Builder / Router / 错误处理）
  eventsourced/              # 事件溯源应用服务（EventSourcedRepository / OutboxRepository / History / Projection / Registry）

data/                        # 数据访问层
  db/                        # 数据库抽象（IDatabase / BasicDB）
    dialect/                 # 方言抽象（DELETE LIMIT 等兼容）
    sql/                     # SQL Builder（Select/Insert/Update/Delete/Upsert）
  orm/                       # ORM 抽象（IQueryable / IRepository）

http/                        # HTTP 抽象（IHandler / IRouter / IMiddleware）
  basic/                     # net/http 封装实现

logging/                     # 日志接口与实现（ILogger / StdLogger / ComponentLogger）
di/                          # 依赖注入（IContainer / BasicContainer）
validation/                  # 验证抽象（IValidator）
server/                      # Server 抽象与生命周期管理

patterns/                    # 通用模式
  retry/                     # 重试模式
  saga/                      # Saga 编排（SagaOrchestrator / SagaDefinition）
  workflow/                  # 工作流模式
```

---

## 2. 领域层（domain）——业务语义与模型边界

领域层主要负责领域模型与业务规则的表达，尽量保持对基础设施与具体框架的无感知：不直接依赖 HTTP/数据库/事件总线实现，而是通过抽象接口（如 `errors`、`logging`、`validation`、`eventing` 抽象）协作。

### 2.1 实体与聚合根（`domain/entity`）

实体接口与基础聚合定义在 `domain/entity.go`，事件溯源聚合根在 `domain/eventsourced/entity.go`：

- 基础接口：
  - `IEntity[ID]`：带 ID 与版本的实体；
  - `IDomainEvent`：领域事件接口（只关心事件语义，不关心传输信封），提供 `EventType()`；
  - `IEventSourcedAggregate[ID]`：事件溯源聚合根接口。
- 事件溯源聚合根实现：`EventSourcedAggregate[T comparable]`（`domain/eventsourced/entity.go`）：

```go
type IEventSourcedAggregate[T comparable] interface {
    IEntity[T]
    GetAggregateType() string
    ApplyEvent(evt domain.IDomainEvent) error
    GetUncommittedEvents() []domain.IDomainEvent
    MarkEventsAsCommitted()
}

type EventSourcedAggregate[T comparable] struct {
    // 包含 ID、版本、未提交事件集合；聚合本身不做内部加锁，由应用服务在命令处理层保证对单个聚合实例的串行访问
}
```

使用方式（简化示意）：

```go
type BankAccount struct {
    *eventsourced.EventSourcedAggregate[int64]
    Balance int
}

func NewBankAccount(id int64) *BankAccount {
    return &BankAccount{
        EventSourcedAggregate: eventsourced.NewEventSourcedAggregate[int64](id, "BankAccount"),
    }
}

func (a *BankAccount) ApplyEvent(evt domain.IDomainEvent) error {
    switch e := evt.(type) {
    case *MoneyDeposited:
        a.Balance += e.Amount
    }
    return a.EventSourcedAggregate.ApplyEvent(evt)
}
```

### 2.2 仓储接口（`domain/repository`）

`domain/repository` 提供标准仓储抽象：

- `IRepository[T, ID]`：基础 CRUD；
- `IBatchOperations[T, ID]`：批量操作；
- `IQueryableRepository[T, ID]`：基于 `QueryOptions` 的过滤/排序/分页；
- `ITransactional`：事务接口。

示意（简化）：

```go
type IRepository[T entity.IEntity[ID], ID comparable] interface {
    GetByID(ctx context.Context, id ID) (T, error)
    List(ctx context.Context, offset, limit int) ([]T, error)
    Count(ctx context.Context) (int64, error)
    Create(ctx context.Context, entity T) error
    Update(ctx context.Context, entity T) error
    Delete(ctx context.Context, id ID) error
}

type QueryOptions struct {
    Offset    int
    Limit     int
    Filters   map[string]any
    OrderBy   string
    OrderDesc bool
}
```

### 2.3 事件溯源服务（`domain/eventsourced` + `app/eventsourced`）

事件溯源能力分为两层：

**领域层（`domain/eventsourced`）**：定义接口与核心协议，不依赖基础设施
- `IEventSourcedAggregate[ID]`：事件溯源聚合根接口（包含 ApplyEvent / GetUncommittedEvents 等方法）
- `EventSourcedAggregate[T]`：通用聚合基类，实现版本控制与未提交事件管理
- `IEventSourcedRepository[T, ID]`：仓储接口（Save / GetByID / Exists / GetAggregateVersion）
- `EventSourcedService[T]`：命令执行模板，封装"加载聚合 → 执行命令 → 保存聚合"流程（仅依赖仓储接口与日志）
- `IEventSourcedCommand`：命令接口，提供 `AggregateID()` 方法用于定位聚合

**应用层（`app/eventsourced`）**：提供具体实现与扩展能力，依赖 eventing 基础设施
- `EventSourcedRepository[T]`：基于 `eventing/store.IEventStore[int64]` 的仓储实现
  - 负责在领域事件（`domain.IDomainEvent`）与存储事件（`eventing.Event`）之间转换
  - 支持快照（Snapshot）、事件发布（EventBus）等可选能力
- `OutboxRepository[T]`：Outbox 模式装饰器，确保事件持久化与消息发布的一致性
- `History`：事件历史查询（`GetEventHistory` / `GetEventHistoryPage` / `EventHistoryMapper`）
- `Projection`：投影辅助工具
- `Registry`：自动注册器（`EventSourcedAutoRegistrar`），简化事件处理器与投影注册
- `CommandAdapter`：将 `EventSourcedService` 适配为 `messaging.IMessageHandler`

示意代码：

```go
// domain/eventsourced - 领域接口
type IEventSourcedCommand interface{ AggregateID() int64 }

type EventSourcedCommandHandler[T IEventSourcedAggregate[int64]] func(
    ctx context.Context, cmd IEventSourcedCommand, aggregate T,
) error

// app/eventsourced - 应用实现：IEventStore[int64] 基于基础设施的实现
storeAdapter, _ := appeventsourced.NewDomainEventStore[*Account, int64](
    appeventsourced.DomainEventStoreOptions[*Account, int64]{
        AggregateType: "account",
        Factory:       NewAccount,
        EventStore:    eventStore, // eventing/store.IEventStore[int64]
        EventBus:      eventBus,   // eventing/bus.IEventBus（可选）
        PublishEvents: true,
    },
)

// domain/eventsourced - 默认仓储实现
repo, _ := eventsourced.NewEventSourcedRepository[*Account](
    "account",
    NewAccount,
    storeAdapter,
)
```

---

## 3. 事件层（eventing）

### 3.1 事件模型（`eventing/event.go`）

事件模型围绕 `IEvent` / `ITypedEvent[ID]` / `IStorableEvent[ID]` / `Event[ID]`：

```go
// IEvent：用于总线/投影等场景的“ID 无关”事件视图
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

// Event[ID]：泛型事件实现，ID 可以是 int64/string/自定义类型
type Event[ID comparable] struct {
    messaging.Message
    AggregateID   ID
    AggregateType string
    Version       uint64
    SchemaVersion int
}
```

`Event[ID]` 同时实现传输与存储接口，支持 SchemaVersion 与元数据（Metadata）。当前框架内置实现主要以 `ID=int64` 形式使用（如 `Event[int64]`），但接口层已经完全支持自定义 ID 类型。

### 3.2 事件存储接口（`eventing/store`）

核心接口：聚合级 `IEventStore[ID]`（`eventing/store/eventstore.go`）：

```go
// ID 为聚合根 ID 类型（如 int64/string/自定义类型）
type IEventStore[ID comparable] interface {
    AppendEvents(ctx context.Context, aggregateID ID, events []eventing.IStorableEvent[ID], expectedVersion uint64) error
    LoadEvents(ctx context.Context, aggregateID ID, afterVersion uint64) ([]eventing.Event[ID], error)
    StreamEvents(ctx context.Context, fromTime time.Time) ([]eventing.Event[ID], error)
}
```

扩展接口：

- `IAggregateInspector[ID]`：检查聚合是否存在/当前版本；
- `IEventStoreExtended[ID]`：基于游标的事件流分页；
- `IAggregateEventStore[ID]`：按聚合顺序流式读取；
- `IEventStreamStore[ID]`：组合 `IEventStoreExtended[ID]` 与 `IAggregateEventStore[ID]`，既支持全局游标流，又支持基于版本的聚合顺序流（用于投影/History 场景）。

内置实现（内存/SQL/缓存/指标）目前实例化为 `ID=int64` 的形式，例如：

```go
memStore := store.NewMemoryEventStore()                // 实现 IEventStore[int64] + IEventStreamStore[int64]
sqlStore := sql.NewSQLEventStore(db, "event_store")    // 实现 IEventStore[int64] + IEventStreamStore[int64]
cached   := cached.NewCachedEventStore(memStore, nil)  // 实现 IEventStoreExtended[int64]
```

SQL 实现基于 `data/db` 与 `data/db/sql.ISql`，位于 `eventing/store/sql`（此处仅说明抽象层，具体 schema 见迁移指南）。

### 3.2.1 聚合 ID 类型策略

事件存储接口在类型层已经完全泛型化（`IEventStore[ID]` / `Event[ID]`），但实际使用中仍需要根据业务与存储后端选择合适的 ID 形态。

**默认推荐：`int64` 自增或雪花 ID**

- 适用场景：
  - 绝大多数基于关系型数据库的服务；
  - 以 SQL 表作为主事件存储的场景（`eventing/store/sql.SQLEventStore`）。
- 优点：
  - 与现有 SQL 示例与索引结构（`aggregate_id INTEGER`）完全兼容；
  - 与 `codegen/snowflake` 生成的 ID 组合良好；
  - 性能与索引友好，易于排查。

**字符串/UUID ID（`string`）**

- 适用场景：
  - 业务主键天然是字符串，且需要跨系统对齐（如账号号、业务单号）；
  - 采用 UUID 作为主键，希望事件流中直接使用 UUID 而不是额外的数值映射。
- 使用方式：
  - 在业务仓库或示例中实现 `IEventStore[string]`（如 `examples/domain/eventsourced_stringid.StringMemoryEventStore`）；
  - 通过 `app/eventsourced.DomainEventStore[T, string]` 适配到领域层 `IEventStore[string]`；
  - 再通过 `domain/eventsourced.EventSourcedRepository[T, string]` 组合完成仓储；
  - SQL 场景下，可将事件表中的 `aggregate_id` 列从 `INTEGER` 调整为 `TEXT`，并保持 `aggregate_type, aggregate_id, version` 的联合唯一索引（具体 DDL 见 `docs/migration-guide.md`）。
- 注意事项：
  - 为保持兼容性，框架内置的 SQL EventStore 仍以 `int64` 形态提供实现；使用字符串 ID 的团队需要在业务侧实现对应的 `IEventStore[string]` 或基于现有实现包装一层；
  - 并发冲突错误类型（如 `eventing.ConcurrencyError`）仍以 `int64` 聚合 ID 为主，可在自定义实现中使用 `NewStoreFailedError` 或业务自定义错误类型表达冲突信息。

**其他强类型 ID（自定义 struct）**

- 适用场景：
  - 希望通过强类型（如 `type OrderID struct{ Value string }`）约束 ID 使用；
  - 对事件流的序列化/反序列化有定制需求。
- 使用方式：
  - 在事件载荷与 EventStore 实现中统一使用该强类型作为 `ID`；
  - 保证该类型支持作为 map key（`comparable`），并在序列化时稳定映射为数据库中的字符串或数值列。

**实践建议：**

- 新项目若无特殊需求，仍推荐使用 `int64` 作为默认聚合 ID，以便直接复用当前 SQL EventStore 与 Outbox 实现；
- 需要字符串/UUID ID 时，优先在业务仓库实现 `IEventStore[ID]` 并参考 `examples/domain/eventsourced_stringid` 的组合方式；
- 多种 ID 策略可以在同一系统中并存，但建议在单个上下文（bounded context）内保持统一，以减少跨上下文迁移复杂度。

### 3.3 投影管理（`eventing/projection`）

投影接口与管理器：

- `IProjection`：定义投影最小接口（`manager.go` 同文件开头）；
- `ProjectionManager`：统一管理投影的注册、状态、检查点与事件处理，并通过 `eventing/bus.IEventBus` 订阅事件；
- 检查点接口：`ICheckpointStore`（`checkpoint.go`），可选 SQL 实现：`checkpoint_sql.go`。

`ProjectionManager` 特性：

- 使用内部 `RWMutex` 保证 `statuses` map 与状态字段的并发安全；
- 通过 `ProjectionStatus` 跟踪 `LastEventID/LastEventTime/ProcessedEvents/FailedEvents/LastError`；
- 每次事件处理：
  - 在 handler 外部调用 `projection.Handle`；
  - 成功时递增 `ProcessedEvents` 并更新检查点；
  - 失败时递增 `FailedEvents` 并记录 `LastError`，调用 DeadLetter 钩子。

### 3.4 Outbox 与监控

- Outbox 位于 `eventing/outbox`，基于 `data/db/sql` 实现 SQL 仓储（`sql_repository.go`），提供入库、查询待发布事件、标记已发布/死信等能力；
- 监控辅助位于 `eventing/monitoring.go` 与 `eventing/monitoring/` 子目录，用于统计 EventStore/Outbox 等指标。

---

## 4. 消息系统（messaging）

### 4.1 消息与处理器

- `messaging.Message` / `IMessage`：通用消息结构（ID、Type、Timestamp、Payload、Metadata）；
- `IMessageHandler`：处理器接口：

```go
type IMessageHandler interface {
    Handle(ctx context.Context, message IMessage) error
    Type() string
}
```

### 4.2 MessageBus 与中间件

`messaging/MessageBus`（`messaging/bus.go`）：

- 对外接口：`IMessageBus`，支持 `Subscribe/Unsubscribe/Publish/PublishAll/Use`；
- 内部通过 `Transport` 抽象与具体传输实现解耦；
- 支持中间件链（`IMiddleware`），以及处理器错误钩子 `HandlerErrorHook`。

### 4.3 传输实现（`messaging/transport`）

当前提供：

- `memory`：基于内存队列 + worker 池的异步传输；
- `sync`：同步传输（调用 `Publish` 时在当前 goroutine 内执行 handler）。

本仓库不再内置 NATS/Redis 等具体网络传输实现，推荐在业务仓库中基于 `messaging.ITransport` 自行封装，例如：

- `internal/transport/natsjetstream`：封装 `github.com/nats-io/nats.go`；
- `internal/transport/redisstreams`：封装 `github.com/redis/go-redis/v9`。

所有传输都实现统一的 `Transport` 接口（见 `messaging/transport.go`）。

### 4.4 命令总线（`messaging/command`）

命令是 `Message` 的特化，`Command` 携带聚合 ID、命令类型、租户等元数据。`CommandBus` 是对 `IMessageBus` 的薄包装：

- 不再模拟“同步总线”；同步/异步由底层 `Transport` 决定；
- 聚合锁、幂等性、验证等通过 `messaging/command/middleware` 提供；
- 与 `domain/eventsourced.EventSourcedService` 通过 `AsCommandMessageHandler` 适配。

---

## 5. 应用层与 HTTP 抽象——模板与 API 适配

应用层位于领域层之上，负责将领域模型与仓储组合为可复用的“应用服务模板”，并通过 HTTP 等接口对外暴露 API。应用层依赖领域抽象与 HTTP 抽象，但不直接依赖具体 Web 框架或数据库实现。

### 5.1 应用服务模板（`app/application` 与 `domain/application`）

`Application[T]` 在 `app/application` 中实现标准应用服务模板，基于 `domain/entity` + `domain/repository` 提供：

- 扩展基础 CRUD 服务，提供：
  - `ListByQuery`：基于 `QueryParams` 的列表查询；
  - `ListPage`：分页查询（支持过滤/排序）；
  - `CountByQuery`：条件统计；
  - 批量操作：`CreateBatch`、`UpdateBatch`、`DeleteBatch`；
- 配置结构 `ServiceConfig`：
  - `MaxBatchSize`：批量写入上限；
  - `MaxPageSize`：分页单页上限（与批量写入独立）；
- 其他开关：`AutoValidate`、`EnableCache`、`EnableAudit` 等。

注意：

- 当仓储不支持 `IQueryableRepository` 时，带过滤/排序的 query/page 操作会返回 `ErrQueryableRepositoryRequired`，不会再静默忽略条件；
- `domain/application` 仅作为兼容入口：通过类型别名与构造函数转发到 `app/application`，避免领域层承载具体模板实现，新代码推荐直接导入 `gochen/app/application`。

### 5.2 HTTP 抽象与 API 构建（`http` + `http/basic` + `app/api`）

- `http` 提供基础 HTTP 抽象（上下文、请求、响应），以及追踪/租户中间件；
- `http/basic` 提供基于 `net/http` 的默认实现（`HttpServer` + `HttpContext`），负责路由注册与中间件链执行，不直接承载具体业务逻辑；
- `app/api` 提供简化的 RESTful 构建器，将 Application 或领域服务暴露为 HTTP API，并与 `errors.Normalize` 协作统一错误返回；
- 上层业务项目可以：
  - 直接使用 `http/basic.HttpServer` 作为默认 HTTP 容器；
  - 或在本地实现 `http.IHttpServer` 适配 Gin/Fiber/Echo 等框架，再交给 `server.Server` 与 `app/api` 做组合。

从分层角度看：

- **领域层（domain/**）定义业务模型与仓储接口，不关心 HTTP 或具体框架；
- **应用层（app/**）在领域之上提供 Application 模板与 API 构建，仅依赖 HTTP 抽象与错误/日志接口；
- **接口适配层（http/** + `server.Server`）只负责“怎么对外提供 API”，不参与业务决策。

---

## 6. 数据库抽象与 SQL Builder

### 6.1 基础 DB 抽象（`data/db/basic`）

定义统一的 `IDatabase` 接口，并提供最简实现，屏蔽具体驱动差异。

### 6.2 SQL Builder（`data/db/sql`）

模块化的 SQL 构建层：

- `ISql` 接口支持 Select/Insert/Update/Delete/Upsert；
- 每类语句拆分到独立文件（`select.go`、`insert.go` 等）；
- 结合 `data/db/dialect.Dialect` 处理不同数据库（MySQL/SQLite/Postgres）的方言差异（DELETE LIMIT、Upsert 语法等）。

事件存储、Outbox、投影检查点等 SQL 实现都优先基于这一层构建。

---

## 7. Saga 与通用模式（patterns/*）——纯模式与流程

### 7.1 Saga（`saga`）

提供 Saga 编排器，基于命令与消息总线实现跨聚合长事务：

- 支持按步骤定义补偿逻辑；
- 状态持久化接口：`state_store.go` + 内存实现 `state_store_memory.go`；
- 配合 `messaging/command` 与 `eventing` 使用。

### 7.2 通用模式（`patterns/retry`、`patterns/workflow`）

- `patterns/retry` 提供简单的重试封装，可用于包装命令、事件处理或数据库操作；
- `patterns/workflow` 提供轻量的流程/状态机能力，可以在应用层组合使用；
- 这些模式包的设计目标是 **不直接依赖具体领域模型或基础设施实现**，而是通过接口（如 `messaging/command`、`eventing`）进行协作，保持“纯模式”属性，便于复用与演进。

---

## 8. 错误与日志

### 8.1 错误设计与错误码

gochen 在错误设计上采用“**错误码 + 结构化错误类型 + 哨兵 + 工厂函数**”的组合模式，核心思路：

- 核心错误包：`gochen/errors`
  - 定义统一的应用错误接口 `errors.IError` 与错误码枚举 `errors.ErrorCode`；
  - 使用 `AppError` 承载错误码、消息、堆栈与上下文（Details），通过 `NewError` / `NewErrorWithCause` / `WrapError` 等工厂函数创建实例；
  - 为常见错误预定义哨兵（`ErrInternal()`、`ErrNotFound()` 等），仅用于 `errors.Is` 比较，不捕获堆栈，业务代码应优先使用 `NewXxxError` 创建带堆栈的实例。

- 领域与基础设施错误：
  - `domain/errors.go` 定义领域错误码 `domain.ErrorCode` 与 `RepositoryError` 类型，分为：
    - 仓储操作错误：`ENTITY_NOT_FOUND`、`ENTITY_ALREADY_EXISTS`、`INVALID_ID`、`VERSION_CONFLICT`、`REPOSITORY_FAILED`；
    - 实体状态错误：`ALREADY_DELETED`、`NOT_DELETED`、`INVALID_VERSION`（例如审计/软删场景）。
  - `eventing/errors.go` 定义事件存储错误码 `eventing.ErrorCode` 与 `EventStoreError` 类型，如：
    - `AGGREGATE_NOT_FOUND`、`INVALID_EVENT`、`EVENT_NOT_FOUND`、`STORE_FAILED`、`EVENT_ALREADY_EXISTS` 等。
  - `patterns/saga/errors.go` 定义 Saga 错误码 `saga.ErrorCode` 与 `SagaError` 类型，用于表达：
    - `SAGA_NOT_FOUND`、`SAGA_INVALID_STATE`、`SAGA_STEP_FAILED`、`SAGA_COMPENSATION_FAILED`、`SAGA_ALREADY_COMPLETED` 等，并在错误中附带 `SagaID`、`StepName` 与 `Cause`。

- 哨兵与工厂函数（统一模式）：
  - 每个错误类型（`RepositoryError` / `EventStoreError` / `SagaError` / `AppError`）都提供：
    - 私有哨兵变量（例如 `errEntityNotFound`），以及导出的访问函数（例如 `domain.ErrEntityNotFound()`）——用于 `errors.Is` 比较；
    - 若干 `NewXxxError(...)` 工厂函数——用于创建带错误码与上下文的实例，内部调用点会捕获堆栈。
  - 结构化错误类型都实现了：
    - `Error() string`：人类可读消息；
    - `Unwrap() error`：保留底层 `Cause`，便于 `errors.Unwrap` 链式处理；
    - `Is(target error) bool`：基于错误码匹配，使 `errors.Is` 能按“错误类别”而不是“指针相等”工作（例如 `errors.Is(NewNotFoundError(...), domain.ErrEntityNotFound())`）。

- 使用建议：
  - 领域层与应用层：
    - 在仓储/服务边界上使用 `domain.NewNotFoundError`、`domain.NewAlreadyExistsError` 等工厂函数创建错误，并根据需要附加实体 ID、预期/实际版本等上下文；
    - 在审计/软删实体（`domain/audited.Entity`）中使用 `domain.NewAlreadyDeletedError` / `NewNotDeletedError` 表达实体状态问题，而仓储级错误使用 RepositoryError 的仓储错误码；
  - 事件层与 Saga：
    - 事件存储实现使用 `eventing.NewAggregateNotFoundError`、`NewInvalidEventError`、`NewStoreFailedErrorWithEvent` 等构造错误，并尽量提供 EventID / AggregateID / EventType 等信息；
    - Saga 编排器通过 `NewSagaStepFailedError` / `NewSagaCompensationFailedError` / `NewSagaInvalidStateError` 等，将 SagaID、StepName 与底层错误绑定在一起，便于日志与追踪。
  - 判断错误类型：
    - 使用 `errors.Is(err, domain.ErrEntityNotFound())` / `errors.Is(err, eventing.ErrAggregateNotFound())` / `errors.Is(err, saga.ErrSagaStepFailed())` 判断错误类别；
    - 使用 `errors.As(err, *domain.RepositoryError)` / `errors.As(err, *eventing.EventStoreError)` 拿到具体错误对象，进一步检查错误码、实体 ID 或事件信息。

该错误设计在保证错误码一致性的同时，通过结构化类型与 `errors.Is/As` 的组合提供了良好的可诊断性和可扩展性。

### 8.2 日志（`logging`）

定义统一的 `logging.ILogger` 接口与基础实现，所有核心组件（eventing、messaging、saga、app 等）通过该接口输出结构化日志，便于统一接入实际日志后台。

### 8.3 监控（`eventing/monitoring`、传输 stats 等）

- `eventing/monitoring` 与 `eventing/monitoring.go` 提供事件与投影相关指标的收集辅助；
- 传输层（如 `messaging/transport/memory`）提供 `Stats` 方法查看队列深度、处理器数量、worker 数等运行状态。

这些监控点可以集成到 Prometheus 或其他监控系统，用于观测 EventStore、Outbox、投影和消息总线的健康状况。

### 8.4 各模块错误使用约定（Error Usage Guidelines）

在统一错误模型（`gochen/errors` + 各模块局部错误类型）的基础上，不同模块在“如何创建/返回/规范化错误”上有各自的约定。本小节给出简要指引，帮助调用方正确使用。

#### 8.4.1 根级错误包（`gochen/errors`）

- **用途**：在 API 边界与日志/监控中统一错误码与载荷。
- **创建方式**：
  - 在应用入口或 HTTP 层优先使用 `errors.NewError` / `NewErrorWithCause` / `NewXxxError` 工厂函数创建 `AppError` 实例；
  - 内部模块（domain/eventing/messaging 等）优先使用各自模块错误类型（如 `RepositoryError`、`EventStoreError`），由 HTTP 层通过 `errors.Normalize` 统一映射为 `AppError`。
- **判定方式**：
  - 使用 `errors.Is(err, errors.ErrNotFound())` / `errors.IsValidation(err)` 等工具函数做错误类别判断；
  - 需要细粒度信息时使用 `errors.As(err, *errors.AppError)` 提取结构化错误。

#### 8.4.2 领域层（`domain/*`）

- **错误类型**：`domain/errors.go` 中的 `RepositoryError` + `ErrorCode`（实体不存在、已存在、ID 无效、版本冲突等），以及少量审计/软删场景的状态错误码。
- **使用约定**：
  - 仓储实现应在“实体不存在/已存在/版本冲突”等场景下返回对应的 `RepositoryError`，并携带实体 ID、预期/实际版本等上下文；
  - 领域服务在边界处原样透传 `RepositoryError`，由上层进行归类与规范化，而不是在领域层直接拼装 HTTP 状态码；
  - 调用方判断领域错误时使用 `errors.Is(err, domain.ErrEntityNotFound())` 等形式，避免直接比较字符串消息。

#### 8.4.3 事件层（`eventing/*`）

- **错误类型**：`eventing/errors.go` 中的 `EventStoreError`（`AGGREGATE_NOT_FOUND`、`EVENT_ALREADY_EXISTS`、`INVALID_EVENT` 等）以及 SQL 实现中的包装错误。
- **使用约定**：
  - EventStore 实现（内存/SQL）在遇到并发冲突、事件重复、聚合不存在等情况时，优先返回对应的 `EventStoreError`，并在 Details 中附带 AggregateID/EventID 等信息；
  - 调用方在判断“聚合不存在”时统一使用 `errors.Is(err, eventing.ErrAggregateNotFound())`，避免通过 `len(events)==0` 自行推断；
  - 在 `DomainEventStore.RestoreAggregate` 与 `Exists` 内部，底层 `ErrAggregateNotFound` 会被转换为“聚合不存在且非错误”（返回版本 0 或 false），调用方无需再做一次错误码分支判断。

#### 8.4.4 消息系统（`messaging/*`）

- **错误来源**：
  - 订阅/退订相关错误：由底层 `IMessageBus` 或 `ITransport` 返回，通常为配置错误或传输层故障；
  - 处理器错误：由业务 handler 返回，语义上属于“业务错误”，CommandBus 在同步 Transport 下会透传 handler 错误。
- **使用约定**：
  - 注册 handler 失败（Subscribe/Unsubscribe 返回错误）应视为装配阶段错误，调用方应立即 fail-fast（例如在 `main` 中 log.Fatal 或 panic），避免带着部分注册的总线继续运行；
  - CommandBus/MessageBus 不对 handler 错误做重试或吞噬，调用方应在 handler 内部或中间件中根据业务需要将错误转换为 `gochen/errors.AppError` 或记录监控指标；
  - 对于需要幂等或有副作用的命令，推荐结合幂等中间件与领域层的 ErrorCode（例如“版本冲突”）做重试/告警。

#### 8.4.5 应用层与 HTTP 抽象（`app/*`、`http/*`）

- **应用层（`app/application`、`app/eventsourced`）**：
  - Application/DomainEventStore 等组件在持久化失败、事件发布失败等场景下应返回结构化错误，并保留底层 `cause`，避免只返回裸字符串；
  - 在 Outbox 模式下，DomainEventStore 将 Outbox 仓储错误原样向上返回，由调用方决定是否重试或降级；
  - 构造函数（如 `NewDomainEventStore`）在配置缺失或组合不安全（例如 `PublishEvents=true` 但未配置 OutboxRepo）时必须直接返回错误，阻止应用在“危险配置”下启动。

- **HTTP 抽象（`http/basic`）**：
  - `HttpUtils.WriteErrorResponse` 统一通过 `errors.Normalize` 将任意错误映射为 `AppError`，并据此选择 HTTP 状态码；
  - Handler 内部推荐只关注业务错误（领域/事件/验证），不直接拼装 HTTP 响应；异常情况交由 HttpUtils/中间件统一处理；
  - 若在 handler 中确实需要提前写出响应，应通过 `ctx.Set("response_written", true)` 标记，避免重复输出。

#### 8.4.6 模式层（`patterns/*`）与示例代码（`examples/*`）

- **模式层（Saga/Workflow/Retry）**：
  - Saga/Workflow 等组件应在 orchestrator 或 manager 层集中将内部错误包装为各自模块的错误类型（如 `SagaError`），并携带必要上下文（SagaID/StepName 等）；
  - 调用方在边界处可以选择直接暴露模式层错误，也可以在 API 层通过 `errors.Normalize` 统一为 `AppError`。

- **示例代码与 README**：
  - 示例与 README 中若为了简化演示而忽略错误，必须显式标注注释，例如：

    ```go
    // demo only: 为简化示例省略错误处理，生产环境请检查并根据错误码处理返回值
    _ = repo.Save(ctx, aggregate)
    ```

  - 核心示例推荐使用 `must(err)` 或 `if err != nil { log.Fatal(err) }` 等方式显式处理错误，保持与生产实践一致；
  - 在文档中使用的代码片段应尽量展示推荐的错误处理模式，而不是依赖读者自行补全。
