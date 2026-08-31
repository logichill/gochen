# Outbox（eventing/outbox）

Outbox 模式用于保证“写事件 + 发布事件”的可靠性：在同一数据库事务里写入事件与 Outbox 记录，事务外由 Publisher 异步发布到 `eventing/bus`。

## 顶层概念

- `outbox.OutboxEntry[ID]`：Outbox 表行结构（`AggregateID` 支持泛型）。
- `outbox.IOutboxRepository[ID]`：仓储接口（保存/拉取 pending/标记 published/failed/清理）。
- `outbox.Publisher[ID]`：串行发布器（简单可控）。
- `outbox.ParallelPublisher[ID]`：并行发布器（worker pool），支持可选的批量标记（减少 DB 往返）。
- `outbox.IDLQRepository[ID]`：可选死信（失败超过阈值后迁移到 DLQ 表）。

这里的 DLQ 属于“尚未发布成功”的持久 Outbox 恢复流程，支持查询与重投。消息已成功投递、
但 handler 执行失败时使用 `messaging/deadletter.ISink`；后者是处理失败 sink，不维护 Outbox
状态，也不提供自动重投。两种 DLQ 位于链路不同阶段，不能直接互换。

## SQL 实现（内置）

SQL 仓储实现位于 `eventing/outbox/sqlstore`，根包只保留 Outbox 契约、实体与 publisher：

- `sqlstore.NewSimpleSQLOutboxRepository(db, eventStore, logger) (repo, err)`：默认 `ID=int64`。
- `sqlstore.NewSimpleSQLOutboxRepositoryWithCodec[ID](db, eventStore, codec, logger) (repo, err)`：显式指定 `codec.ICodec[ID, any]`（通常由 `idcodec.NewString[...]()` 等实现，用于 `string/UUID/强类型别名` ID）。
- `sqlstore.NewSQLDLQRepository(db, outboxRepo, maxRetries, autoCleanup) (dlq, err)`：默认 `ID=int64`。
- `sqlstore.NewSQLDLQRepositoryWithCodec[ID](db, outboxRepo, codec, maxRetries, autoCleanup) (dlq, err)`：DLQ 泛型 ID + codec。
- `sqlstore.NewBatchOperations(db)`：可选批量标记（发布成功/失败的批量更新）。

## 最小用法（int64）

> 说明：
> - 推荐先直接运行本仓库示例：`go run ./examples/infra/outbox/sql`（示例中包含 DDL 与监控端点演示）。
> - 下方代码用于“可复制运行”的最小 demo（SQLite `:memory:`），演示：写事件+写 outbox → publisher 发布 → handler 收到事件。
> - 示例使用 `memory.Transport`，其 `Publish` 成功只表示消息已入内存队列，适合 demo/test；生产 durable outbox 应使用能表达 broker ack 或 handler 成功语义的 transport。
> - 表结构/索引在不同版本可能演进；生产环境请使用迁移工具，并以 `examples/infra/outbox/sql/internal/schema/schema.go` 的 DDL 为准。

```go
package main

import (
	"context"
	"log"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"

	"gochen/db"
	basicdb "gochen/db/sql/stdsql"
	"gochen/eventing"
	"gochen/eventing/bus"
	"gochen/eventing/outbox"
	outboxsql "gochen/eventing/outbox/sqlstore"
	"gochen/eventing/registry"
	storesql "gochen/eventing/store/sqlstore"
	"gochen/eventing/upcast"
	"gochen/observe/logging"
	"gochen/messaging"
	"gochen/messaging/transport/memory"
)

type TestEventPayload struct {
	Value int `json:"value"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// 1) DB + 表结构（演示用 DDL）
	database, err := basicdb.New(db.DBConfig{Driver: "sqlite", Database: ":memory:"})
	must(err)
	defer func() { _ = database.Close() }()
	must(ensureEventAndOutboxTables(ctx, database))

	// 2) EventStore + Outbox repo
	storeLogger := logging.ComponentLogger("outbox.demo.event_store")
	es, err := storesql.NewSQLEventStore(database, "event_store", storesql.WithLogger(storeLogger))
	must(err)
	repo, err := outboxsql.NewSimpleSQLOutboxRepository(database, es, nil)
	must(err)

	// 3) EventBus（示例使用内存异步 transport）
	tpt := memory.NewMemoryTransport(1024, 4)
	must(tpt.Start(ctx))
	defer func() { _ = tpt.Stop(context.Background()) }()

	messageBus := messaging.NewMessageBus(tpt)
	eventBus := bus.NewEventBus(messageBus)

	// 4) registry/upgraders（必须）：Publisher 会强约束 outbox 中的事件类型必须已注册
	reg := registry.NewRegistry()
	must(reg.Register("TestEvent", func() any { return &TestEventPayload{} }))
	upgraders := upcast.NewUpgraderRegistry()

	// 5) handler：订阅并统计收到的事件
	var received int32
	logger := logging.ComponentLogger("outbox.demo")
	unsub, err := eventBus.SubscribeEvent(ctx, "TestEvent", bus.EventHandlerFunc(func(ctx context.Context, evt eventing.IEvent) error {
		atomic.AddInt32(&received, 1)
		logger.Info(ctx, "received event",
			logging.String("event_id", evt.GetID()),
			logging.String("event_type", evt.GetType()),
		)
		return nil
	}))
	must(err)
	defer func() { _ = unsub(ctx) }()

	// 6) Publisher
	cfg := outbox.DefaultOutboxConfig()
	cfg.PublishInterval = 10 * time.Millisecond
	cfg.BatchSize = 50

	publisher, err := outbox.NewPublisher[int64](repo, eventBus, cfg, nil, reg, upgraders)
	must(err)
	must(publisher.Start(ctx))
	defer func() { _ = publisher.Stop(context.Background()) }()

	// 7) 写端：同一事务写 event_store + event_outbox
	evt := eventing.NewEventWithID[int64]("evt-1", 1, "TestAggregate", "TestEvent", 1, &TestEventPayload{Value: 42})
	must(repo.SaveWithEvents(ctx, 1, []eventing.Event[int64]{*evt}))

	// 8) 等待发布 & 消费
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&received) > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	log.Fatal("event not received")
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func ensureEventAndOutboxTables(ctx context.Context, database db.IDatabase) error {
	// 说明：
	// - 本 DDL 仅用于 README demo 的快速运行；
	// - 生产环境请使用迁移工具，并以 `examples/infra/outbox/sql/internal/schema/schema.go` 的 DDL 为准。
	// event_store（适配 gochen/eventing/store/sqlstore）
	_, err := database.Exec(ctx, `
CREATE TABLE IF NOT EXISTS event_store (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    aggregate_id INTEGER NOT NULL,
    aggregate_type TEXT NOT NULL,
    version INTEGER NOT NULL,
    schema_version INTEGER NOT NULL,
    timestamp DATETIME NOT NULL,
    payload TEXT NOT NULL,
    metadata TEXT NOT NULL,
    UNIQUE(aggregate_id, aggregate_type, version)
);`)
	if err != nil {
		return err
	}

	// event_outbox
	_, err = database.Exec(ctx, `
CREATE TABLE IF NOT EXISTS event_outbox (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    aggregate_id INTEGER NOT NULL,
    aggregate_type TEXT NOT NULL,
    event_id TEXT NOT NULL UNIQUE,
    event_type TEXT NOT NULL,
    event_data TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    claim_token TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    published_at DATETIME NULL,
    retry_count INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NULL,
    lease_until DATETIME NULL,
    next_retry_at DATETIME NULL
);
CREATE INDEX IF NOT EXISTS idx_event_outbox_pending_claim ON event_outbox (status, created_at, id);
CREATE INDEX IF NOT EXISTS idx_event_outbox_retry_claim ON event_outbox (status, next_retry_at, created_at, id);
CREATE INDEX IF NOT EXISTS idx_event_outbox_lease_claim ON event_outbox (status, lease_until, created_at, id);
CREATE INDEX IF NOT EXISTS idx_event_outbox_aggregate ON event_outbox (aggregate_id, aggregate_type);
`)
	return err
}
```

## 监控与健康检查

本包提供两类监控信号：

- **Publisher 埋点（进程内）**：记录条目反序列化/hydration/发布的耗时与错误率（通过可选接口注入）；
- **DB 指标采集（进程外/拉取式）**：默认表名场景使用 `outboxsql.NewMetricsCollector(database)`；自定义表名场景优先使用 `outboxsql.NewMetricsCollectorForRepository(database, repo)`（repo 需实现 `outboxsql.IOutboxTableProvider`，SQL repo 可通过 `outboxsql.WithOutboxTable` 配置），否则显式调用 `outboxsql.NewMetricsCollectorWithTables(database, outboxTable, dlqTable)`。

如果你使用 `eventing/monitoring`，可以将两者汇总到一个 HTTP 端点：

```go
collector, err := outboxsql.NewMetricsCollectorForRepository(database, repo)
must(err)
provider, err := outboxmon.NewProvider(collector)
must(err) // must 可沿用上方 demo 中的实现

reg, err := monitoring.NewRegistry(
	monitoring.WithOutboxMetricsProvider(
		provider,
	),
)
must(err)
must(monitoring.SetDefaultRegistry(reg))

publisher.SetMetricsRecorder(reg.Metrics)

http.Handle("/internal/monitoring/", http.StripPrefix("/internal/monitoring",
	monitoring.NewHTTPHandlerWithRoutes(reg, monitoring.FullRouteSet())))
```

> 说明：该片段需要额外导入 `net/http`、`gochen/eventing/monitoring`，`outboxmon` 别名：`gochen/eventing/outbox/outboxmon`，以及 `outboxsql` 别名：`gochen/eventing/outbox/sqlstore`。

## 使用非 int64 聚合 ID（string/强类型别名）

> 说明：基于上面示例，只需要把 ID 类型与 codec 替换即可（其余 `eventBus/reg/upgraders/cfg` 等保持一致）；记得额外导入 `gochen/codec/idcodec`，SQL EventStore 仍使用 `gochen/eventing/store/sqlstore`。

```go
type AccountID string

idCodec := idcodec.NewString[AccountID]()

es, err := storesql.NewSQLEventStoreWithCodec[AccountID](db, "event_store", idCodec)
must(err)
repo, err := outboxsql.NewSimpleSQLOutboxRepositoryWithCodec[AccountID](db, es, idCodec, nil)
must(err)

publisher, err := outbox.NewPublisher[AccountID](repo, eventBus, cfg, nil, reg, upgraders)
must(err)
```

## 语义与实践建议

- Outbox 默认语义为“至少一次”（At-Least-Once）。消费者应具备幂等性（可使用 `message.ID`/`event.ID` 做幂等键）。
- 反序列化失败、载荷 hydration 失败、发布失败会被标记为 failed 并指数退避重试；超过 `MaxRetries` 可配置迁移到 DLQ。SQL DLQ repository 会优先通过 `MarkFailedAndMoveToDLQ` 在同一事务内校验当前 claim、标记 failed 并迁入 DLQ；直接 `MoveToDLQ` 只接受真实 failed 行。若 DLQ `autoCleanup=false`，原 Outbox 行会进入 `dead_lettered` 终态并不再被 claim。
- `RetryFromDLQ` 会把 failed/dead_lettered outbox 行恢复为 pending 并删除 DLQ 记录；若原 outbox 行仍是 pending/processing，则返回 `Conflict` 并保留 DLQ，避免覆盖正在处理的记录。
- 如果自定义 `ClaimLease`，请使用同一份 `OutboxConfig` 创建 SQL repository 与 publisher；publisher 会在构造时校验两边租约，避免续约节奏与实际 lease 漂移。
- 续租（keepalive）失败会作为独立错误上报，但不会直接丢弃已成功的发布：若 `PublishEvent` 已返回成功，publisher 仍会优先 best-effort `MarkAsPublished`，以降低条目被重新 claim 后重复投递的概率。该过程不提供 exactly-once 保证；若 `MarkAsPublished` 最终失败，条目可能被恢复为 `failed` 并在退避到期后重新发布，因此消费者仍必须按 `event.ID` 幂等处理。
- `SaveWithEvents` 对已存在且内容一致的 Outbox `event_id` 视为幂等重放；若同一 `event_id` 对应内容不同，则返回 `Duplicate`。
- 表结构/索引建议以 `examples/infra/outbox/sql/internal/schema/schema.go` 为准，并为 `status/next_retry_at`、`aggregate_id/aggregate_type` 建索引。

## 并发与线程安全（契约）

### 1) Repository 的并发语义

- `IOutboxRepository` 的实现应支持并发调用（典型为 DB 连接池/事务实现提供保障）。
- Outbox 是“至少一次”链路：如果多个 Publisher 实例并发拉取同一批 pending 条目，仍可能产生重复发布；系统层面应通过 `event_id/message_id` 做幂等（存储侧唯一约束或消费侧去重）。

### 2) Publisher/ParallelPublisher 生命周期

- `Publisher/ParallelPublisher` 推荐作为进程级单例使用（一个 outbox 表通常只需要一个发布器实例）。
- `Start(ctx)` / `Stop(ctx)` 支持并发调用且幂等。
  - Stop-before-Start 为 no-op（不会影响后续 Start）。
  - 一旦 Start 过并进入停止流程（显式 `Stop(ctx)`，或 `ctx.Done()` 导致后台退出），即视为 terminal：**不可再次 Start**（需要重启请创建新实例）。
- 串行 `Publisher` 内部会串行化单次发布流程：避免后台 loop 与 `PublishPending` 并发触发导致重复发布。
- 所有并发 `Stop(ctx)` 调用等待同一个完成屏障；完成前会等待已进入的手工 `PublishPending`、worker drain 和最终标记 flush，调用方可在 Stop 成功后关闭 bus/repository。
- `ParallelPublisher.PublishPending` 需要先 `Start`；未启动会返回 `FAILED_PRECONDITION`。
  - `ParallelPublisher.Start(ctx)` 会监听 `ctx.Done()` 并自动触发 `Stop(ctx)`，因此取消 ctx 会使实例进入 terminal。

### 3) 顺序语义

- `ParallelPublisher` 通过按 `aggregate_type + aggregate_id` 分片（shard）保证 **同一聚合** 内的发布顺序尽量稳定（不同聚合之间不保证顺序）。
- 如果业务要求全局严格顺序，应避免并行发布，或在消费侧用业务序列化策略收敛。
