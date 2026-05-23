# eventing/store：事件存储（EventStore）

`eventing/store` 提供事件存储的核心抽象与默认实现（内存/SQL/缓存装饰器/快照），用于 Event Sourcing 的“追加事件 + 回放事件”链路。

## 顶层概念

- `store.IEventStore[ID]`：聚合级事件存储（Append/Load/LoadByType/版本/存在性）。
- `store.IEventStreamStore[ID]`：事件流扫描接口（游标/limit），用于投影回放、历史导出等“全局扫描”场景。
- 默认实现：
  - `store.NewMemoryEventStore()`：内存实现（默认 `ID=int64`）。
  - `store/sqlstore`：SQL 实现（默认 `ID=int64`，支持 codec 扩展）。
  - `store/cached`：缓存装饰器（在 inner store 上叠加读缓存/统计/TTL）。
  - `store/snapshot`：快照存储与策略（减少回放事件量）。

## 并发与线程安全（契约）

### 1) Store 实例可并发复用

除非实现另有说明，`IEventStore/IEventStreamStore` 的实现应满足：

- **可并发调用**：在多 goroutine 中复用同一个 store 实例调用 `AppendEvents/LoadEvents/StreamEvents/...` 不产生数据竞态；
- **一致性由后端保证**：内存实现依赖内部锁；SQL 实现依赖数据库事务/约束（例如 `(aggregate_type, aggregate_id, version)` 的唯一性）；
- **错误语义一致**：并发冲突返回 `errors.Concurrency`，且在 `Details` 中携带 `aggregate_id/expected_version/actual_version` 等关键字段（见 `contract_concurrency_test.go`）。

### 2) `expectedVersion` 是并发控制边界

`AppendEvents(ctx, aggregateID, events, expectedVersion)` 使用乐观并发控制：

- 调用方应基于 `LoadEvents/GetAggregateVersion` 得到的版本号填写 `expectedVersion`；
- 当并发写入导致版本不匹配时，返回 `errors.Concurrency`；
- “至少一次”投递（Outbox/消息重放）场景应保证幂等：可用 `event_id`/`message_id` 做去重键（存储侧或消费侧）。

### 3) 事件流扫描不保证快照一致性

`StreamEvents` 面向投影/回放，通常以“游标 + limit”分页读取：

- 可能与写入并发，**不保证快照一致性**；
- 调用方应按游标推进，并处理“重复/漏读”边界（例如以 `(timestamp,id)` 作为稳定排序键）。

## 回归测试

- 并发冲突错误码契约：`eventing/store/contract_concurrency_test.go`
- 缓存装饰器并发：`eventing/store/cached/cached_store_concurrency_test.go`
