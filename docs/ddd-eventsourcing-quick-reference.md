# DDD + Event Sourcing + CQRS 快速参考（gochen 版）

本指南基于当前仓库结构，演示如何使用：

- 领域层：`domain/entity`（事件溯源聚合根）、`domain/eventsourced`（命令服务 / 仓储模板）
- 事件层：`eventing/event.go`、`eventing/store`、`eventing/bus`、`eventing/projection`、`eventing/outbox`
- 消息层：`messaging` 与 `messaging/command`

构建一个从命令 → 聚合 → 事件存储 → 事件总线 → 投影的完整链路。

> 所有代码片段均为**简化示意**，接口/类型名与当前代码保持一致，但为便于阅读省略了错误处理与配套实现。

---

## 1. 事件溯源聚合根（domain/entity）

### 1.1 定义聚合根

文件位置：`domain/entity/aggregate_eventsourced.go`

```go
type BankAccount struct {
    *entity.EventSourcedAggregate[int64]
    Balance int64
}

func NewBankAccount(id int64) *BankAccount {
    return &BankAccount{
        EventSourcedAggregate: entity.NewEventSourcedAggregate[int64](id, "BankAccount"),
    }
}

// ApplyEvent 应用事件
func (a *BankAccount) ApplyEvent(evt eventing.IEvent) error {
    switch evt.GetType() {
    case "MoneyDeposited":
        amount := evt.GetPayload().(int64)
        a.Balance += amount
    case "MoneyWithdrawn":
        amount := evt.GetPayload().(int64)
        a.Balance -= amount
    }
    // 基类负责版本号递增等通用处理
    return a.EventSourcedAggregate.ApplyEvent(evt)
}
```

### 1.2 引发事件

聚合不直接修改外部存储，而是“应用并记录”事件：

```go
func (a *BankAccount) Deposit(amount int64) error {
    if amount <= 0 {
        return fmt.Errorf("amount must be positive")
    }

    evt := eventing.NewDomainEvent(
        a.GetID(),        // AggregateID
        a.GetAggregateType(),
        "MoneyDeposited", // event type
        uint64(a.GetVersion()+1),
        amount,           // payload
    )

    return a.ApplyAndRecord(evt)
}
```

---

## 2. 事件存储（eventing/store）

### 2.1 IEventStore 抽象

文件位置：`eventing/store/eventstore.go`

```go
type IEventStore interface {
    AppendEvents(ctx context.Context, aggregateID int64, events []eventing.IStorableEvent, expectedVersion uint64) error
    LoadEvents(ctx context.Context, aggregateID int64, afterVersion uint64) ([]eventing.Event, error)
    StreamEvents(ctx context.Context, fromTime time.Time) ([]eventing.Event, error)
}
```

典型 SQL 实现会定义事件表（示意）：

```sql
CREATE TABLE event_store (
  id             BIGINT PRIMARY KEY auto_increment,
  aggregate_id   BIGINT       NOT NULL,
  aggregate_type VARCHAR(64)  NOT NULL,
  event_type     VARCHAR(128) NOT NULL,
  version        BIGINT       NOT NULL,
  schema_version INT          NOT NULL,
  payload        JSON         NOT NULL,
  timestamp      TIMESTAMP    NOT NULL,
  UNIQUE KEY uk_aggregate_version (aggregate_id, version)
);
```

---

## 3. 事件溯源仓储与服务（domain/eventsourced）

### 3.1 事件溯源仓储

文件位置：`domain/eventsourced/repository.go`

核心职责：

- 使用 `IEventStore` 加载聚合事件流；
- 调用 `LoadFromHistory` 重建聚合状态；
- 将未提交事件通过 `AppendEvents` 持久化。

```go
type EventSourcedRepository[T entity.IEventSourcedAggregate[int64]] struct {
    store store.IEventStore
    // 省略类型工厂等
}

func (r *EventSourcedRepository[T]) GetByID(ctx context.Context, id int64) (T, error) {
    // 1. 创建空聚合
    agg := newAggregateInstance[T](id)

    // 2. 从 EventStore 加载历史事件
    events, err := r.store.LoadEvents(ctx, id, 0)
    if err != nil {
        return agg, err
    }

    // 3. 重放事件
    if err := agg.LoadFromHistory(events); err != nil {
        return agg, err
    }
    return agg, nil
}

func (r *EventSourcedRepository[T]) Save(ctx context.Context, agg T) error {
    events := agg.GetUncommittedEvents()
    if len(events) == 0 {
        return nil
    }
    storable := make([]eventing.IStorableEvent, len(events))
    for i, e := range events {
        storable[i] = e.(eventing.IStorableEvent)
    }
    if err := r.store.AppendEvents(ctx, agg.GetID(), storable, uint64(agg.GetVersion())); err != nil {
        return err
    }
    agg.MarkEventsAsCommitted()
    return nil
}
```

### 3.2 命令服务模板

文件位置：`domain/eventsourced/service.go`

统一模板 `EventSourcedService[T]` 封装“加载聚合 → 执行命令 → 保存聚合”的流程：

```go
type IEventSourcedCommand interface {
    AggregateID() int64
}

type EventSourcedCommandHandler[T entity.IEventSourcedAggregate[int64]] func(
    ctx context.Context,
    cmd IEventSourcedCommand,
    aggregate T,
) error

func (s *EventSourcedService[T]) ExecuteCommand(ctx context.Context, cmd IEventSourcedCommand) error {
    aggID := cmd.AggregateID()
    agg, err := s.repository.GetByID(ctx, aggID)
    if err != nil {
        return err
    }

    handler := s.handlers[reflect.TypeOf(cmd)]
    execErr := handler(ctx, cmd, agg)
    if execErr != nil {
        return execErr
    }

    return s.repository.Save(ctx, agg)
}
```

---

## 4. 事件总线与命令总线（eventing/bus + messaging/command）

### 4.1 事件总线

文件位置：`eventing/bus/eventbus.go`

事件总线是 `messaging.MessageBus` 的类型安全包装：

```go
type IEventHandler interface {
    messaging.IMessageHandler
    HandleEvent(ctx context.Context, evt eventing.IEvent) error
    GetEventTypes() []string
    GetHandlerName() string
}

type IEventBus interface {
    messaging.IMessageBus
    PublishEvent(ctx context.Context, evt eventing.IEvent) error
    PublishEvents(ctx context.Context, events []eventing.IEvent) error
    SubscribeEvent(ctx context.Context, eventType string, handler IEventHandler) error
}
```

### 4.2 命令总线

文件位置：`messaging/command`

命令封装在 `Command` 结构体中，命令总线通过 `MessageBus` 路由：

```go
// 简化示意
cmd := command.NewCommand("SetBalance", aggregateID, payload)
cmdBus.Dispatch(ctx, cmd)
```

`domain/eventsourced.EventSourcedService` 提供 `AsCommandMessageHandler`，将命令处理适配为 `IMessageHandler`，供命令总线订阅。

---

## 5. 投影与 CQRS 读模型（eventing/projection）

### 5.1 投影接口与 ProjectionManager

文件位置：`eventing/projection/manager.go`

```go
type IProjection interface {
    GetName() string
    Handle(ctx context.Context, event eventing.IEvent) error
    GetSupportedEventTypes() []string
    Rebuild(ctx context.Context, events []eventing.Event) error
    GetStatus() ProjectionStatus
}

type ProjectionManager struct {
    // 管理投影、订阅事件总线、跟踪状态与检查点
}
```

ProjectionManager 特点：

- 通过 `eventing/bus.IEventBus` 订阅指定事件类型；
- 使用 `ProjectionStatus` 记录每个投影的运行状态：
  - `ProcessedEvents` / `FailedEvents`；
  - `LastEventID` / `LastEventTime`；
  - `Status`（running/stopped/error）与 `LastError`。
- 状态字段在内部由 `RWMutex` 保护，避免并发竞态。

### 5.2 定义投影

```go
type AccountBalanceProjection struct {
    db IDatabase
}

func (p *AccountBalanceProjection) GetName() string { return "account_balance" }

func (p *AccountBalanceProjection) GetSupportedEventTypes() []string {
    return []string{"MoneyDeposited", "MoneyWithdrawn"}
}

func (p *AccountBalanceProjection) Handle(ctx context.Context, evt eventing.IEvent) error {
    switch evt.GetType() {
    case "MoneyDeposited":
        // 更新读模型表 balances
    case "MoneyWithdrawn":
        // 更新读模型表 balances
    }
    return nil
}
```

注册投影：

```go
pm := projection.NewProjectionManager(eventStore, eventBus).
    WithCheckpointStore(checkpointStore)

_ = pm.RegisterProjection(&AccountBalanceProjection{db: db})
_ = pm.StartProjection("account_balance")
```

---

## 6. Outbox 模式（eventing/outbox）

Outbox 模式用于在同一事务内写入领域事件和“待发布消息”，再由独立 Outbox 发布器异步发送到外部消息系统。

文件位置：`eventing/outbox`

- `OutboxEntry`：Outbox 表行结构；
- `IOutboxRepository`：定义存取接口；
- `sql_repository.go`：基于 `storage/database/sql.ISql` 的 SQL 实现；
- `publisher.go` / `publisher_parallel.go`：串行/并行发布器实现；
- `dlq.go`：死信队列支持。

使用时，可在事件存储或应用服务中将领域事件转换为 OutboxEntry，在单个数据库事务内完成写入，然后由独立任务（或进程）周期性扫描并发布 Outbox。

---

## 7. 整体流程回顾

一个典型的 ES + CQRS 流程在 gochen 中大致如下：

1. **命令到达**：HTTP API / 消息订阅 → 创建 `Command` → 通过 `CommandBus` 或 `EventSourcedService.ExecuteCommand` 调用。
2. **加载聚合**：`EventSourcedRepository` 使用 `IEventStore.LoadEvents` 重放历史事件，构建聚合。
3. **执行命令**：聚合方法（如 `Deposit`）调用 `ApplyAndRecord` 生成领域事件。
4. **持久化事件**：`EventSourcedRepository.Save` 调用 `IEventStore.AppendEvents` 写入事件存储。
5. **发布事件**：可选地将提交的事件推送到事件总线或 Outbox。
6. **更新投影**：`ProjectionManager` 订阅事件总线，按需更新读模型表，并维护检查点与状态。
7. **读取模型**：应用或 API 直接使用读模型仓储（普通 `IRepository`）进行高效查询。

通过上述组件的组合，gochen 在不强制绑定具体框架/ORM 的前提下，提供了一套相对完整的 DDD + Event Sourcing + CQRS 基础设施。  
进一步的细节（如具体 SQL schema、迁移实践）可参考 `migration-guide.md` 与 `eventing` 相关源代码。

