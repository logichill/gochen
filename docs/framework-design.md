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
- **框架无侵入**：不绑定具体 Web 框架或 ORM，HTTP/数据库通过 `httpx` 和 `storage/database` 抽象；
- **并发安全**：关键组件（消息总线、事件存储、投影管理器等）在设计上显式考虑并发。

### 1.2 目录结构概览

当前根目录核心结构：

```text
domain/                      # 领域层抽象
  entity/                    # 实体 & 聚合根（含 EventSourcedAggregate）
  eventsourced/              # 事件溯源服务/仓储模板
  repository/                # 仓储接口（CRUD + Query + Batch 等）
  service/                   # 领域服务基础抽象

eventing/                    # 事件系统
  event.go                   # 事件模型（IEvent / IStorableEvent / Event）
  store/                     # 事件存储接口与扩展
  projection/                # 投影接口与 ProjectionManager + Checkpoint
  outbox/                    # Outbox 仓储与发布器
  bus/                       # IEventBus（基于 messaging.MessageBus）
  monitoring.go              # 监控辅助

messaging/                   # 消息总线与传输层
  message.go                 # IMessage / Message
  handler.go                 # IMessageHandler
  bus.go                     # MessageBus 实现 + 中间件
  command/                   # 命令模型与命令总线适配
  transport/                 # 传输实现（memory / natsjetstream / redisstreams / sync）

storage/database/            # 数据库抽象
  basic/                     # 最小 DB 接口实现
  dialect/                   # 方言抽象（DELETE LIMIT 等兼容）
  sql/                       # ISql Builder（Select/Insert/Update/Delete/Upsert）

app/                         # 应用服务层
  application.go             # Application 泛型服务 + 查询/分页
  api/                       # HTTP API 构建器（路由器、错误处理等）

httpx/                       # HTTP 抽象
di/                          # 依赖注入接口（IContainer + BasicContainer）
logging/                     # 日志接口与基础实现
saga/                        # Saga 编排
patterns/retry/              # 重试模式
```

---

## 2. 领域层（domain）

### 2.1 实体与聚合根（`domain/entity`）

实体接口和基类定义在 `domain/entity`：

- 基础接口：
  - `IEntity[ID]`：带 ID 与版本的实体；
  - `IAggregate[ID]`：聚合根，额外提供 `GetAggregateType`、`GetDomainEvents` 等；
  - `IEventSourcedAggregate[ID]`：事件溯源聚合根接口。
- 事件溯源聚合根实现：`EventSourcedAggregate[T comparable]`（`domain/entity/aggregate_eventsourced.go`）：

```go
type IEventSourcedAggregate[T comparable] interface {
    IAggregate[T]
    ApplyEvent(evt eventing.IEvent) error
    GetUncommittedEvents() []eventing.IEvent
    MarkEventsAsCommitted()
    LoadFromHistory(events []eventing.IEvent) error
}

type EventSourcedAggregate[T comparable] struct {
    // 包含 ID、版本、未提交事件集合，并发通过 RWMutex 保护
}
```

使用方式（简化示意）：

```go
type BankAccount struct {
    *entity.EventSourcedAggregate[int64]
    Balance int
}

func NewBankAccount(id int64) *BankAccount {
    return &BankAccount{
        EventSourcedAggregate: entity.NewEventSourcedAggregate[int64](id, "BankAccount"),
    }
}

func (a *BankAccount) ApplyEvent(evt eventing.IEvent) error {
    switch evt.GetType() {
    case "MoneyDeposited":
        a.Balance += evt.GetPayload().(int)
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

### 2.3 事件溯源服务（`domain/eventsourced`）

`domain/eventsourced` 提供基于 EventStore 的统一模板：

- `EventSourcedRepository[T]`：使用 `eventing/store.IEventStore` 重建聚合并持久化事件；
- `EventSourcedService[T]`：命令执行模板，封装“加载聚合 → 执行命令 → 保存事件 → 发布事件”的流程（`domain/eventsourced/service.go`）；
- 与 `messaging/command.Command` 的适配器：`AsCommandMessageHandler` 用于将服务暴露为 `IMessageHandler`。

示意：

```go
type IEventSourcedCommand interface{ AggregateID() int64 }

type EventSourcedCommandHandler[T entity.IEventSourcedAggregate[int64]] func(
    ctx context.Context,
    cmd IEventSourcedCommand,
    aggregate T,
) error
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

SQL 实现基于 `storage/database` 与 `storage/database/sql.ISql`，位于 `eventing/store/sql`（此处仅说明抽象层，具体 schema 见迁移指南）。

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

- Outbox 位于 `eventing/outbox`，基于 `storage/database/sql` 实现 SQL 仓储（`sql_repository.go`），提供入库、查询待发布事件、标记已发布/死信等能力；
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

## 5. 应用层与 HTTP 抽象

### 5.1 应用服务（`app/application.go`）

`Application[T]` 在 `domain/repository` 上提供标准应用服务：

- 扩展基础 CRUD 服务，提供：
  - `ListByQuery`：基于 `QueryParams` 的列表查询；
  - `ListPage`：分页查询（支持过滤/排序）；
  - `CountByQuery`：条件统计；
  - 批量操作：`CreateBatch`、`UpdateBatch`、`DeleteBatch`；
- 配置结构 `ServiceConfig`：
  - `MaxBatchSize`：批量写入上限；
  - `MaxPageSize`：分页单页上限（与批量写入独立）；
  - 其他开关：`AutoValidate`、`EnableCache`、`EnableAudit` 等。

注意：当仓储不支持 `IQueryableRepository` 时，带过滤/排序的 query/page 操作会返回 `ErrQueryableRepositoryRequired`，不会再静默忽略条件。

### 5.2 HTTP 抽象与 API 构建（`httpx` + `app/api`）

- `httpx` 提供基础 HTTP 抽象（上下文、请求、响应），以及追踪/租户中间件；
- `app/api` 提供简化的 RESTful 构建器，将 `Application` 或领域服务暴露为 HTTP API，并与 `errors.Normalize` 协作统一错误返回。

---

## 6. 数据库抽象与 SQL Builder

### 6.1 基础 DB 抽象（`storage/database/basic`）

定义统一的 `IDatabase` 接口，并提供最简实现，屏蔽具体驱动差异。

### 6.2 SQL Builder（`storage/database/sql`）

模块化的 SQL 构建层：

- `ISql` 接口支持 Select/Insert/Update/Delete/Upsert；
- 每类语句拆分到独立文件（`select.go`、`insert.go` 等）；
- 结合 `storage/database/dialect.Dialect` 处理不同数据库（MySQL/SQLite/Postgres）的方言差异（DELETE LIMIT、Upsert 语法等）。

事件存储、Outbox、投影检查点等 SQL 实现都优先基于这一层构建。

---

## 7. Saga 与通用模式

### 7.1 Saga（`saga`）

提供 Saga 编排器，基于命令与消息总线实现跨聚合长事务：

- 支持按步骤定义补偿逻辑；
- 状态持久化接口：`state_store.go` + 内存实现 `state_store_memory.go`；
- 配合 `messaging/command` 与 `eventing` 使用。

### 7.2 通用模式（`patterns/retry`）

`patterns/retry` 提供简单的重试封装，可用于包装命令、事件处理或数据库操作。

---

## 8. 监控与日志

### 8.1 日志（`logging`）

定义统一的 `logging.Logger` 接口与基础实现，所有核心组件（eventing、messaging、saga 等）通过该接口输出结构化日志，便于统一接入实际日志后台。

### 8.2 监控（`eventing/monitoring`、传输 stats 等）

- `eventing/monitoring` 与 `eventing/monitoring.go` 提供事件与投影相关指标的收集辅助；
- 传输层（如 `messaging/transport/memory`）提供 `Stats` 方法查看队列深度、处理器数量、worker 数等运行状态。

这些监控点可以集成到 Prometheus 或其他监控系统，用于观测 EventStore、Outbox、投影和消息总线的健康状况。
