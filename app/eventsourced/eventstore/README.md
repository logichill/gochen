# 事件存储适配

本包把领域事件端口连接到 EventStore、Outbox、Schema 升级和快照能力。只使用自定义 `domain/eventsourced.IDomainEventStore` 的调用方无需导入本包。

组合根创建 `NewDomainEventStore(DomainEventStoreOptions[ID])`，把返回的领域存储端口注入 `app/eventsourced.NewEventSourcedRepository`。存储适配只接收聚合 ID 类型参数；聚合类型参数 T 留在领域仓储。`EventHistory*` 类型与查询函数归 `app/eventsourced`。

使用 Runtime 的常规装配可直接调用 `quick.EventSourced` / `quick.AtomicEventSourced`，只声明一次 AggregateType，见 [Quick 装配](../../../docs/guides/quick-assembly.md#事件溯源)。

## 消费接口

`DomainEventStoreOptions[ID].EventStore` 只要求本包 `IAggregateEventStore[ID]` 的三个方法：`AppendEvents`、`GetAggregateVersion`、`StreamAggregate`。自定义后端不必实现全局扫描或一次性加载历史。

普通历史查询依赖 `store.IEventReader`；分页历史依赖 `store.IAggregateEventReader`，包含聚合版本与分页读取；投影回放依赖 `store.IEventStreamReader`。SQL Outbox 的 `AppendEventsWithDB` 协作接口由 Runtime 消费侧定义。完整存储可以直接传入上述端口。

迁移时将 `DomainEventStoreOptions[T, ID]`、`NewDomainEventStore[T, ID]`、`NewAtomicDomainEventStore[T, ID]` 的类型参数改为 `[ID]`，同步收窄自定义后端和测试替身；不保留旧泛型签名。

## 快照与提交结果

快照恢复通过 `ISnapshotLoader`，快照创建通过 `ISnapshotWriter` 注入；`eventing/store/snapshot.Manager` 满足这些接口，调用方也可提供自己的实现。接口中的 typed nil 按未启用快照处理。

直接发布模式先追加事件，发布失败返回 `contextx.AfterCommitError`；快照装饰器的 `FailOnError=true` 同样在主保存成功后返回该标记。错误仍携带原始 cause，调用方应处理发布或快照失败，而不把整个业务当成未提交。需要可靠投递时通过 Outbox 原子保存事件与待发布记录。

## 显式发布模式

`DomainEventStoreOptions.PublishMode` 必填，零值无效。旧 `PublishEvents` 字段已移除，不提供兼容推断。

| 模式 | EventBus | OutboxRepo | 行为 |
| --- | --- | --- | --- |
| `PublishModePersistOnly` | 禁止 | 禁止 | 只追加事件 |
| `PublishModeDirect` | 必需 | 禁止 | 追加成功后直接发布 |
| `PublishModeOutbox` | 禁止 | 必需 | 原子保存事件与 Outbox，后台发布 |

三种模式都需要 EventStore、事件 ID 生成器、Registry 和 UpgraderRegistry。缺失、typed nil 必需依赖或矛盾发布依赖返回 InvalidInput；禁止字段应保持 nil 接口。显式选择 Direct 后，不再输出“退化到直接发布”的构造日志。

下面展示在共同配置 `opts` 上选择一种模式；每个示例分别使用，不要叠加：

```go
// 只持久化：不传发布依赖。
opts.PublishMode = esstore.PublishModePersistOnly

// 直接发布：eventBus 必须非空。
opts.PublishMode = esstore.PublishModeDirect
opts.EventBus = eventBus

// Outbox：outboxRepo 必须非空，不向适配器传 EventBus。
opts.PublishMode = esstore.PublishModeOutbox
opts.OutboxRepo = outboxRepo
```

直接发布失败仍返回 AfterCommitError，事件已保存；Outbox 写入失败不退回直接追加。批量能力通过 `NewAtomicDomainEventStore` 单独构造，仅支持 PersistOnly；不支持的模式或存储在构造时返回错误。普通构造器返回 `IDomainEventStore`，原子构造器返回 `IAtomicDomainEventStore`；具体适配器与快照装饰器不导出。

迁移时应按业务选择模式：以前的 `PublishEvents=false` 且没有 Outbox 对应 PersistOnly，并移除未使用的 EventBus；已有 Outbox 对应 Outbox，EventBus 留给后台 Publisher；直接发布对应 Direct，缺少总线的装配必须修正。

启用快照时装饰原子仓储会在构造阶段被拒绝；未配置 writer 时保持原实例。
