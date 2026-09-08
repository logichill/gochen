# Outbox

Outbox 将事件与待发布记录原子保存，再由 Publisher 在事务外发布到 EventBus。它提供至少一次投递语义，消费者需按 event ID 幂等处理。

## 组件与存储

| Core 入口 | 职责 |
| --- | --- |
| `OutboxEntry[ID]` | 聚合、事件、发布状态、重试和租约信息 |
| `IOutboxRepository[ID]` | SaveWithEvents、claim、续租、成功 / 失败标记、清理 |
| `Publisher[ID]` | 串行发布 |
| `ParallelPublisher[ID]` | worker 分片发布与可选批量标记 |
| `IDLQRepository[ID]` | 发布失败记录的查询、删除与重投 |
| `NewMemoryOutboxRepository(eventStore)` | 纯内存实现，与 DomainEventStore 共用同一 MemoryEventStore |

SQL 实现在 `gochen-runtime` 仓库 `eventing/outbox/sqlstore`，包括 SQL Outbox、DLQ、批量标记、清理和指标采集。其事件 Store 必须支持同事务追加；默认聚合 ID 为 int64，其他 ID 类型通过带 codec 的构造函数声明。

## 发布流程

```text
SaveWithEvents → pending → claim / processing → published
                                  ↓
                                failed → 重试或 DLQ
```

- `ClaimPendingEntries` 原子获取记录，返回 claim token 与租约。
- MarkAsPublished、MarkAsFailed、RenewClaim 都核对 token，避免其他 worker 更新当前占用。
- Publisher 在发布期间续租；SQL repository 与 Publisher 使用一致的规范化 OutboxConfig，构造期校验 lease。
- 载荷通过显式注入的 Registry / UpgraderRegistry 还原，反序列化、升级或发布失败进入失败处理。
- 发布成功但最终状态保存失败仍可能再次投递；续租失败也不能据此推断消息未发布。

Transport 的成功含义决定可靠性边界。内存异步队列只保证入队；需要持久投递时应使用可确认 broker 持久接收的 Transport，或符合业务要求的同步处理路径。

## 死信与重投

SQL DLQ 可在同一事务中校验 claim、标记 failed 并迁入 DLQ。直接 MoveToDLQ 只接受 failed 行；autoCleanup=false 时原 Outbox 行保留为 dead_lettered 终态。

RetryFromDLQ 将 failed / dead_lettered 记录恢复为 pending 并删除死信。若原记录仍是 pending / processing，返回 Conflict 并保留 DLQ，避免覆盖正在执行的发布。相同 event ID 与相同内容的 SaveWithEvents 可幂等复用，内容冲突返回 Duplicate。

Outbox DLQ 处理“尚未发布成功”的记录；`messaging/deadletter.ISink` 处理“已交给 handler 但处理失败”的快照，不管理 Outbox 状态或自动重投。

## 生命周期与并发

Repository 应支持并发调用，多 Publisher 可通过 claim 协调；仍需接受至少一次投递。

Publisher 的 `Start(ctx)` / `Stop(ctx)` 可并发且幂等。启动前 Stop 不影响后续启动；启动后进入停止流程即为终止态，需要重新运行时创建新实例。Stop 成功后才关闭共享总线与存储。

串行 Publisher 会串行化后台循环与手动 PublishPending。ParallelPublisher 需要先 Start 才能 PublishPending；按 aggregate type + ID 分片有助于同聚合顺序，但不提供跨实例或全局严格顺序保证。

## Schema、监控与示例

表与索引要求集中见[数据库 Schema 与迁移](../../docs/guides/db-schema-migration-guide.md#outbox)。SQL DDL 的可执行示例位于 `gochen-runtime` 仓库 `examples/infra/outbox/sql/internal/schema/schema.go`。

在 Runtime 仓库执行：

```bash
GOWORK=off go run ./examples/infra/outbox/sql
```

监控可组合 Publisher metrics recorder、SQL MetricsCollector 与 Core monitoring Registry。配置自定义表名时，指标与清理组件必须使用同一表配置。
