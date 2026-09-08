# EventStore

`eventing/store` 定义聚合事件存储、全局流扫描与事务追加契约。Core 提供内存实现、缓存装饰器和快照能力；SQL 实现在 `gochen-runtime` 仓库 `eventing/store/sqlstore`。

## 接口与实现

| 入口 | 内容 |
| --- | --- |
| `IEventStore[ID]` | AppendEvents、LoadEvents、HasAggregate、GetAggregateVersion |
| `IEventStreamStore[ID]` | 在基础接口上增加 StreamEvents / StreamAggregate |
| `ITransactionalEventStore[ID]` | 使用调用方数据库 / 事务作用域执行 AppendEventsWithDB |
| `NewMemoryEventStore[ID]()` | 内存事件存储 |
| `store/cached` | 读缓存、TTL 与统计 |
| `store/snapshot` | 快照存储契约、内存实现与策略 |

完整签名见 [eventstore.go](eventstore.go)。

## 聚合与并发

聚合由 `(aggregateType, aggregateID)` 唯一定位，aggregateType 必须非空。不同聚合类型可复用相同 ID，版本各自递增。

`AppendEvents` 的 expectedVersion 是该事件流上一次已提交版本，新聚合为 0。版本检查与追加必须原子执行；冲突返回 `errors.Concurrency`，错误 details 携带聚合和版本信息。批次中的 nil / typed-nil 事件返回 `InvalidInput`。

实现应支持并发调用。缓存装饰器按聚合 generation 阻止并发写入前读取的旧值回填，读返回事件快照。

## 事件流与游标

`StreamEvents(ctx, opts)` 返回 Events、NextCursor、EventCursors 和 HasMore。游标由存储实现定义，调用方应原样保存；逐条提交消费进度时优先使用与 Events 一一对应的 EventCursors。不存在的事件 ID 游标返回 `NotFound`。

默认与最大页大小均为 1000。`StreamAggregate` 按聚合版本读取，返回 NextVersion。全局扫描可与写入并发，不提供整个分页过程的快照事务；可靠消费须结合后端顺序能力、持久游标与幂等处理。

Runtime SQL store 可使用 `global_position` 与分配器建立全局顺序，也支持按 timestamp / ID 扫描。生产 schema 与索引要求见[数据库 Schema 与迁移](../../docs/guides/db-schema-migration-guide.md#事件存储)。

## 验证

并发冲突契约见 [contract_concurrency_test.go](contract_concurrency_test.go)，缓存并发行为见 `store/cached` 测试。在 Core 仓库执行 `GOWORK=off go test -count=1 ./eventing/store/...`。
