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
- `EventSourcedRepository[T]`：基于 `eventing/store.IEventStore` 的仓储实现
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

// app/eventsourced - 应用实现：IEventStore 基于基础设施的实现
storeAdapter, _ := appeventsourced.NewDomainEventStore[*Account](
    appeventsourced.DomainEventStoreOptions[*Account]{
        AggregateType: "account",
        Factory:       NewAccount,
        EventStore:    eventStore, // eventing/store.IEventStore
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

事件模型围绕 `IEvent` / `IStorableEvent` / `Event`：

```go
type IEvent interface {
    messaging.IMessage
    GetAggregateID() int64
    GetAggregateType() string
    GetVersion() uint64
}

type IStorableEvent interface {
    IEvent
    GetSchemaVersion() int
    SetAggregateType(string)
    Validate() error
}
```

`Event` 同时实现传输与存储接口，支持 SchemaVersion 与元数据（Metadata）。

### 3.2 事件存储接口（`eventing/store`）

核心接口：`IEventStore`（`eventing/store/eventstore.go`）：

```go
type IEventStore interface {
    AppendEvents(ctx context.Context, aggregateID int64, events []eventing.IStorableEvent, expectedVersion uint64) error
    LoadEvents(ctx context.Context, aggregateID int64, afterVersion uint64) ([]eventing.Event, error)
    StreamEvents(ctx context.Context, fromTime time.Time) ([]eventing.Event, error)
}
```

扩展接口：

- `IAggregateInspector`：检查聚合是否存在/当前版本；
- `IEventStoreExtended`：基于游标的事件流分页；
- `IAggregateEventStore`：按聚合顺序流式读取。

SQL 实现基于 `data/db` 与 `data/db/sql.ISql`，位于 `eventing/store/sql`（此处仅说明抽象层，具体 schema 见迁移指南）。

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
