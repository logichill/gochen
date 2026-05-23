# DDD + Event Sourcing + CQRS 速查

基于当前仓库结构，演示如何用 gochen 搭建"命令 → 聚合 → 事件存储 → 事件总线 → 投影"的完整链路。

涉及模块：

- 领域层：`domain/crud`（基础仓储端口）、`domain/eventsourced`（事件溯源聚合根与仓储接口）
- 应用层 ES 模板：`app/eventsourced`
- 事件层：`eventing/event.go`、`eventing/store`、`eventing/bus`、`eventing/projection`、`eventing/outbox`
- 消息层：`messaging` 与 `messaging/command`

> 代码片段为**简化示意**，接口/类型名与当前代码一致，但省略了错误处理与配套装配。
>
> 事件 schema 演进与滚动升级纪律见 [`event-schema-evolution.md`](event-schema-evolution.md)。

---

## 1. 事件溯源聚合根（domain/eventsourced）

### 1.1 定义聚合根

文件位置：`domain/eventsourced/entity.go`

```go
type BankAccount struct {
    *eventsourced.EventSourcedAggregate[int64]
    Balance int64
}

func NewBankAccount(registry *eventsourced.MetadataRegistry, id int64) (*BankAccount, error) {
    a := &BankAccount{}
    agg, err := eventsourced.InitAggregate[int64](registry, a, id, "BankAccount")
    if err != nil { return nil, err }
    a.EventSourcedAggregate = agg
    return a, nil
}

func (a *BankAccount) ApplyMoneyDeposited(evt *MoneyDeposited) {
    a.Balance += evt.Amount
}

func (a *BankAccount) ApplyMoneyWithdrawn(evt *MoneyWithdrawn) {
    a.Balance -= evt.Amount
}
```

### 1.2 引发事件

聚合不直接修改外部存储，而是"应用并记录"事件：

```go
func (a *BankAccount) Deposit(amount int64) error {
    if amount <= 0 {
        return fmt.Errorf("amount must be positive")
    }
    return a.ApplyAndRecord(&MoneyDeposited{Amount: amount})
}
```

仓储保存时直接使用聚合暴露的 `GetExpectedVersion()` 作为乐观锁基线版本。

### 1.3 模块启动期统一校验 metadata

如果在模块装配阶段集中声明 aggregates，通过 `host.Module(...).Aggregate(...)` 统一声明：

```go
func NewProgressModule() (host.IModule, error) {
    return host.Module("progress").
        Name("Progress").
        Aggregate(
            host.Aggregate(&Task{}, "task"),
            host.Aggregate(&UserLevel{}, "level"),
            host.Aggregate(&UserPoints{}, "user_points"),
        ).
        Build()
}
```

Host `Module(...).Build()` 阶段做 fail-fast 校验，真正的 metadata 预热发生在模块 `Init()` 装配阶段；运行期构造聚合时优先复用已预热的 metadata，未预热则首次使用时自动 ensure。业务构造函数不再需要显式 metadata bind 样板。

---

## 2. 事件存储（eventing/store）

### 2.1 IEventStore 抽象

```go
type IEventStore[ID comparable] interface {
    AppendEvents(ctx context.Context, aggregateID ID, events []eventing.IStorableEvent[ID], expectedVersion uint64) error
    LoadEvents(ctx context.Context, aggregateID ID, afterVersion uint64) ([]eventing.Event[ID], error)
    LoadEventsByType(ctx context.Context, aggregateType string, aggregateID ID, afterVersion uint64) ([]eventing.Event[ID], error)
    HasAggregate(ctx context.Context, aggregateID ID) (bool, error)
    GetAggregateVersion(ctx context.Context, aggregateID ID) (uint64, error)
}
```

需要"全局事件流扫描 / 投影回放 / 历史导出"时，显式依赖 `IEventStreamStore` 的游标分页接口 `StreamEvents(ctx, opts)`。

SQL 事件表结构见 [`../guides/db-schema-migration-guide.md`](../guides/db-schema-migration-guide.md)。

---

## 3. 事件溯源仓储与命令服务

### 3.1 装配事件溯源仓储

组合方式（推荐）：`eventing/store(IEventStreamStore)` → `app/eventsourced.DomainEventStore` → `app/eventsourced.EventSourcedRepository`。

```go
// demo only：省略错误处理。
reg := registry.NewRegistry()
upgraders := upcast.NewUpgraderRegistry()

eventStore, _ := sqlstore.NewSQLEventStore(db, "event_store", logging.ComponentLogger("eventing.sqlstore"))
_ = eventStore.Init(ctx) // 装配期先 Init/Ping，尽早暴露连接/权限/表结构问题

domainStore, _ := appeventsourced.NewDomainEventStore(appeventsourced.DomainEventStoreOptions[*Account, int64]{
    AggregateType:    "Account",
    EventStore:       eventStore,
    EventBus:         eventBus,   // 可选
    OutboxRepo:       outboxRepo, // 可选，启用 Outbox（生产推荐）
    PublishEvents:    true,
    EventRegistry:    reg,
    UpgraderRegistry: upgraders,
})

repo, _ := appeventsourced.NewEventSourcedRepository[*Account, int64](
    "Account",
    &Account{},
    appeventsourced.AdaptAggregateFactory(NewAccount),
    domainStore,
)
```

### 3.2 命令服务模板

`EventSourcedService[T]` 封装"加载聚合 → 执行命令 → 保存聚合"流程：

```go
type IEventSourcedCommand[ID comparable] interface { AggregateID() ID }

func (s *EventSourcedService[T, ID]) ExecuteCommand(ctx context.Context, cmd IEventSourcedCommand[ID]) error {
    aggID := cmd.AggregateID()
    agg, err := s.repository.GetOrCreate(ctx, aggID)
    if err != nil { return err }

    handler := s.handlers[reflect.TypeOf(cmd)]
    if err := handler(ctx, cmd, agg); err != nil { return err }

    return s.repository.Save(ctx, agg)
}
```

要点：

- 默认 `GetOrCreate` 加载；需要"必须已存在"时在 handler 内检查 `agg.GetVersion() == 0`。
- `EventSourcedServiceOptions.ConcurrencyRetry` 启用"保存阶段并发冲突（`errors.Concurrency`）自动重试"。handler 必须可重入且避免不可回滚的外部副作用。`DefaultRetryConfig()` 默认启用 jitter（`JitterRatio=0.2`）。
- 可选实现 `EventSourcedCommandFinalizeHook.AfterFinalize`，每次 `ExecuteCommand` 无论成功/失败/重试耗尽都只调用一次，提供最终错误与尝试次数。

---

## 4. 事件总线与命令总线

边界记住一句：

- `eventing/bus` 是 `messaging.IMessageBus` 的**事件**语义包装
- `messaging/command` 是 `messaging.IMessageBus` 的**命令**语义包装

两者共享底层传输机制，但承载不同语义。

### 4.1 事件总线

```go
type IEventBus interface {
    messaging.IMessageBus
    PublishEvent(ctx context.Context, evt eventing.IEvent) error
    PublishEvents(ctx context.Context, events []eventing.IEvent) error
    SubscribeEvent(ctx context.Context, eventType string, handler IEventHandler) (messaging.UnsubscribeFunc, error)
    SubscribeHandler(ctx context.Context, handler IEventHandler) (messaging.UnsubscribeFunc, error)
}
```

### 4.2 命令总线

命令投递与执行分开：

```go
// 投递：发到消息总线 / transport
cmd := command.NewCommand("cmd-1", "SetBalance", aggregateID, "Account", payload)
cmdBus.Dispatch(ctx, cmd)

// 执行：在本地当前调用栈内执行
cmdExecutor.Execute(ctx, cmd)
```

`app/eventsourced.EventSourcedService` 提供 `AsCommandMessageHandler`，可将命令处理适配为 `IMessageHandler` 供消息总线订阅；需要同步结果时直接把同一套 handler 装配到 `CommandExecutor`。

---

## 5. 投影与 CQRS 读模型

### 5.1 投影接口

```go
type IProjection[ID comparable] interface {
    Name() string
    Handle(ctx context.Context, event eventing.IEvent) error
    SupportedEventTypes() []string
    Rebuild(ctx context.Context, events []eventing.Event[ID]) error
    Status() ProjectionStatus
}
```

`ProjectionManager` 通过 `IEventBus` 订阅事件类型，用 `ProjectionStatus` 记录 `ProcessedEvents`、`FailedEvents`、`LastEventID`、`LastEventTime`、`Status`、`LastError`。管理器以 per-projection runtime 收拢状态、handler、checkpoint tracker 与内存 cursor，同一 projection 的在线处理、checkpoint 追赶、重放与重建会串行执行。启用 checkpoint 时必须配置 `IEventStreamStore`，在线 handler 只唤醒 runtime 从 EventStore 按 checkpoint cursor 追赶；投影必须实现 `ICheckpointingProjection`，或用 `NewCheckpointingProjector` 包装普通投影，确保读模型写入与 checkpoint 保存处于同一事务边界。

### 5.2 定义投影

```go
type AccountBalanceProjection struct {
    db IDatabase
}

func (p *AccountBalanceProjection) Name() string { return "account_balance" }

func (p *AccountBalanceProjection) SupportedEventTypes() []string {
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

func (p *AccountBalanceProjection) Rebuild(ctx context.Context, events []eventing.Event[int64]) error {
    return nil
}

func (p *AccountBalanceProjection) Status() projection.ProjectionStatus {
    return projection.ProjectionStatus{Name: p.Name()}
}
```

注册：

```go
pm, err := projection.NewProjectionManager[int64](eventStore, eventBus, reg, upgraders)
if err != nil {
    return err
}
pm, err = pm.WithCheckpointStore(checkpointStore)
if err != nil {
    return err
}

txRunner, err := projection.NewSQLCheckpointTxRunner(ormEngine)
if err != nil {
    return err
}
accountBalance, err := projection.NewCheckpointingProjector[int64](
    &AccountBalanceProjection{db: db},
    txRunner,
)
if err != nil {
    return err
}

_ = pm.RegisterProjection(accountBalance)
_ = pm.StartProjection("account_balance")
```

---

## 6. Outbox 模式

用于在同一事务内写入领域事件和"待发布消息"，由独立发布器异步投递到外部消息系统。

位置：`eventing/outbox`。核心组件：

- `OutboxEntry[ID]` — Outbox 表行结构
- `IOutboxRepository[ID]` — 存取接口
- `publisher.go` / `publisher_parallel.go` — 串行/并行发布器
- `dlq.go` — 死信队列

使用时，在事件存储或应用服务中把领域事件转换成 `OutboxEntry`，在单个数据库事务内完成写入；再由独立任务周期性扫描并发布。SQL 表结构见 [`../guides/db-schema-migration-guide.md`](../guides/db-schema-migration-guide.md)。

---

## 7. 整体流程回顾

一个典型的 ES + CQRS 请求在 gochen 中的路径：

1. **命令到达**：HTTP 或消息订阅 → 创建 `Command` → `CommandBus.Dispatch`（投递）或 `CommandExecutor.Execute` / `EventSourcedService.ExecuteCommand`（同步执行）。
2. **加载聚合**：`EventSourcedRepository` 用 `IEventStore.LoadEvents` 重放历史事件，构建聚合。
3. **执行命令**：聚合方法调用 `ApplyAndRecord` 生成领域事件。
4. **持久化事件**：`EventSourcedRepository.Save` → `IEventStore.AppendEvents` 写入事件存储。
5. **发布事件**：可选地把已提交事件推送到事件总线或 Outbox。
6. **更新投影**：`ProjectionManager` 订阅事件总线，按需更新读模型表，维护检查点与状态。
7. **读取模型**：应用或 API 直接用读模型仓储（普通 `IRepository`）查询。

进一步细节：SQL schema 见 [`../guides/db-schema-migration-guide.md`](../guides/db-schema-migration-guide.md)；滚动升级与事件演进纪律见 [`event-schema-evolution.md`](event-schema-evolution.md)。
