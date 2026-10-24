# EventStore

`eventing/store` 定义聚合事件存储、全局流扫描与原子批量追加契约。Core 提供内存实现、缓存装饰器和快照能力；SQL 实现在 `gochen-runtime` 仓库 `eventing/store/sqlstore`。

## 接口与实现

| 入口 | 内容 |
| --- | --- |
| `IEventStore[ID]` | AppendEvents、LoadEvents、HasAggregate、GetAggregateVersion |
| `IEventStreamStore[ID]` | 在基础接口上增加 StreamEvents / StreamAggregate |
| `IEventReader[ID]` | LoadEvents / GetAggregateVersion，供普通历史查询使用 |
| `IAggregateEventReader[ID]` | StreamAggregate / GetAggregateVersion，供恢复与历史分页使用 |
| `IEventStreamReader[ID]` | StreamEvents，供投影回放使用 |
| `IAtomicEventStore[ID]` | 仅 AppendEventsBatch，按需组合其他读取能力 |
| `NewMemoryEventStore[ID]()` | 内存事件存储 |
| `store/cached` | 读缓存、TTL 与统计 |
| `store/decorator` | 上下文、租户、追踪与存储操作指标装饰器 |
| `store/snapshot` | 快照存储契约、内存实现与策略 |

完整签名见 [eventstore.go](eventstore.go) 与 [reader.go](reader.go)。完整后端和缓存装饰器使用组合接口，读取消费者无需实现无关的写入或扫描方法。领域存储适配器的三方法端口见 [app/eventsourced/eventstore](../../app/eventsourced/eventstore/README.md)。

SQL Outbox 的事务协作接口位于 `gochen-runtime` 仓库 `eventing/outbox/sqlstore.IEventAppenderWithDB`，只要求 `AppendEventsWithDB`；数据库句柄不进入 Core 事件存储契约。

## 存储装饰器

`decorator.NewMetricsEventStore(inner, recorder)` 包装任意 `IEventStreamStore`，记录追加、加载与全局流扫描的耗时、事件数和错误状态。记录器实现 `decorator.IMetricsRecorder`，可在组合根与缓存、上下文等装饰器显式组合。

原 `cached.MetricsEventStore`、`cached.NewMetricsEventStore` 和 `cached.IMetricsRecorder` 已迁至 `gochen/eventing/store/decorator`。调用方更新导入路径与包限定名即可，构造签名和运行语义保持一致。

## 聚合与并发

聚合由 `(aggregateType, aggregateID)` 唯一定位，aggregateType 必须非空。不同聚合类型可复用相同 ID，版本各自递增。

`AppendEvents` 的 expectedVersion 是该事件流上一次已提交版本，新聚合为 0。版本检查与追加必须原子执行；冲突返回 `errors.Concurrency`，错误 details 携带聚合和版本信息。批次中的 nil / typed-nil 事件返回 `InvalidInput`。

流仍位于该批次末尾时，相同事件的完整批次重试应幂等成功；不保证后续已有追加时，过期历史批次还能重试成功。重复事件 ID 的内容冲突必须拒绝，不能覆盖已有事实。

实现应支持并发调用。缓存装饰器按 `(aggregateType, aggregateID, contextx.TenantID(ctx))` 隔离读取视图；无租户上下文使用独立视图，不复用租户缓存。所有视图共享 `MaxAggregates` 容量上限。追加成功后失效该聚合的全部视图，并通过 generation 阻止并发写入前读取的旧值回填，读返回事件快照。

## 事件流与游标

`StreamEvents(ctx, opts)` 返回 Events、NextCursor、EventCursors 和 HasMore。游标由存储实现定义，调用方应原样保存；逐条提交消费进度时优先使用与 Events 一一对应的 EventCursors。不存在的事件 ID 游标返回 `NotFound`。

默认与最大页大小均为 1000。`StreamAggregate` 按聚合版本读取，返回 NextVersion。全局扫描可与写入并发，不提供整个分页过程的快照事务；可靠消费须结合后端顺序能力、持久游标与幂等处理。

Runtime SQL store 可使用 `global_position` 与分配器建立全局顺序，也支持按 timestamp / ID 扫描。生产 schema 与索引要求见[数据库 Schema 与迁移](../../docs/guides/db-schema-migration-guide.md#事件存储)。

## 原子批量

`IAtomicEventStore[ID]` 提供 `AppendEventsBatch`，将多个聚合事件流的版本检查与追加作为一次原子操作。Memory 使用锁内私有副本提交，Runtime SQL 使用同一数据库事务；不支持该接口的存储不可用逐项追加模拟原子性。各项沿用单项追加的版本与幂等语义。

批次中的空事件项是严格的版本守卫，失败时整批回滚；它不是普通 `AppendEvents` 空事件调用的无操作语义。调用方应把业务决策依赖的聚合版本一并提交。Runtime 批提交使用 Serializable 事务，使版本守卫与写入构成可串行化操作；不支持该隔离级别的数据库必须报错，不降级隔离级别。

## 验证与实现接入

[contracttest](contracttest/store.go) 提供 `RunEventStore`、`RunEventStream`、`RunAtomic` 三组共享行为测试，覆盖聚合隔离、OCC、幂等、追加原子性、取消、并发写入、游标分页和跨聚合回滚。每组接收一个 factory：按子测试创建空的独立后端，使用 int64 聚合 ID，通过 `t.Cleanup` 释放资源。

Core 内存实现和 Runtime SQL 的 timestamp / global_position 两种模式均运行这些测试。新后端应复用同一套测试，再补充其驱动特有场景；接入示例见 [contract_suite_test.go](contract_suite_test.go)。缓存并发行为见 `store/cached` 测试。在 Core 仓库执行 `GOWORK=off go test -count=1 ./eventing/store/...`。
