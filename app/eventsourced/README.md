# 事件溯源应用模板

`app/eventsourced` 提供领域仓储、事件溯源应用、历史读模型与查询。历史分页直接接收 `store.IAggregateEventReader`；组合根按需选择以下适配包：

| 包 | 内容 |
| --- | --- |
| `app/eventsourced/eventstore` | DomainEventStore、快照仓储装饰器 |
| `app/eventsourced/subscriber` | 事件处理模板、投影模板、处理器/投影注册与清理 |

使用 Runtime 时，反射聚合接入使用 `quick.EventSourced` / `quick.AtomicEventSourced`，统一组合领域存储与仓储，聚合类型只声明一次。发布模式与依赖仍显式提供，示例见 [Quick 装配](../../docs/guides/quick-assembly.md#事件溯源)。仅依赖 Core 或需要自定义领域存储时使用下面的底层构造入口。

聚合通过 `domain/eventsourced.New` 或 `InitAggregate` 构造，处理器统一命名为 `ApplyXxx`。启动时注册并校验 metadata；命令方法负责校验，处理器仅进行确定性的内存状态更新。签名校验无法证明业务回放正确，接入时应执行[聚合使用指南](../../domain/eventsourced/README.md)中的回放验证。

单聚合使用 `NewEventSourcedRepository`，不暴露批量能力。原子批量使用 `eventstore.NewAtomicDomainEventStore` 和 `NewAtomicEventSourcedRepository`：构造阶段要求 `PublishModePersistOnly` 及底层 `store.IAtomicEventStore`。`IAtomicEventSourcedRepository.SaveAll` 成功后统一清除待提交事件，拒绝重复聚合 ID，不会循环单项保存来模拟原子性。

传给 `SaveAll` 的无待提交事件聚合会作为版本守卫保留，验证相关读取未在并发操作中变更。调用方仍需收集完整读取依赖；单独提交写集合不能防止业务判断基于过期的其他聚合。

保存返回 `contextx.AfterCommitError` 表示主保存已经完成、后续阶段失败。仓储仍会标记相应事件已提交，`Application` 同时返回已提交聚合与该错误，并将其排除在并发重试之外，即使配置了宽泛的自定义错误分类器。Operation 将其表达为 degraded 终态，调用方应核对结果或补偿失败阶段，不能直接重跑整个业务命令。

## 应用入口

`NewApplication(repo, Options)` 是反射聚合的标准业务入口：

- `Get`：回放聚合并按 `read` 规约校验当前状态；
- `Execute`：修改已有聚合，保存冲突时按 `ConcurrencyRetry` 重试，命令函数必须可重入；
- `ExecuteNew`：新建聚合，要求回调产出的聚合 `GetExpectedVersion()` 为零，不做并发重试；
- `EventHistory`：配置 `History` 后按 `event_history_read` 规约读取事件历史。

授权器返回的规约在内存中对回放后的聚合求值：读入状态不匹配时 `Get` / `Execute` / `EventHistory` 返回 NotFound。命令执行成功后依次做赋值核对、`domain.IValidatable` 与 `Options.Validator` 校验，再检查最终状态的写规约；`Execute` / `ExecuteNew` 的写规约不匹配时返回 Forbidden，并保持事件流原状。`ExecuteNew` 收到已有聚合时返回 InvalidInput。操作人、动作、原因与关联 ID 写入事件元数据，历史条目直接读取这些字段。命令总线接入使用 `AsExecuteHandler` / `AsExecuteNewHandler`；`Execute` 不提供跨聚合原子保证，多聚合写入使用原子仓储。

事件溯源 Application 在回放后的聚合上求值 `spec.Spec`，判定通过后执行命令和保存。领域事件仓储的 `Get` / `Save` 接收聚合身份和状态。

## 事件历史

`EventHistoryEntry`、`EventHistoryPage`、`EventHistoryMapper` 和查询函数均位于本包。`Options.History`、`LoadEventHistoryPage`、`LoadEventHistoryEntries` 接收 `store.IAggregateEventReader`，读取聚合版本和分页事件；`EventHistory` / `EventHistoryAfter` 接收 `store.IEventReader`。

条目保存事件、聚合、版本、时间及操作人等元数据，业务通过 mapper 提供摘要。分页按聚合类型和 ID 区分事件流，并校验偏移与整数溢出。

`Application.EventHistory` 先按聚合当前状态授权。直接调用历史 helper 时，调用方负责建立访问边界。

## 显式状态路径

新模型可使用 `domain/eventsourced.Model[S,E]` 与 `NewStateRepository(model, stream)`。`eventstore.NewEventStream` 复用已有 DomainEventStore 的 registry、upcaster、信封、expectedVersion 追加、Direct/Outbox 发布，不创建另一套存储。没有新包依赖边。

流程为 `Load(ctx, id)` → `Execute(ctx, baseline, decide)` → 在基线副本决定事件 → 从原基线归约 → `AppendEvents(expectedVersion)`。`State.Value()` 返回深拷贝，`Version()` 是不可改写的乐观锁基线。决定函数可以通过闭包接收具体命令类型，无需 any 命令解释器。不存在的流返回零版本初态，可用同一流程创建。

- Clone 必须正确复制可变引用；构造器拒绝省略 Clone，但 Go 无法证明其正确性。决定函数修改自己的状态副本不会直接提交，只有返回事件影响结果。
- 决定、归约或追加失败时不改变旧基线；成功返回新基线。AfterCommitError 同时返回已提交新基线，不能重试同批事件。无自动重试，冲突后业务重新加载并重新授权、决定。
- 空事件不调用追加，也不进行版本守卫；需要跨流原子守卫时使用原子仓储，不把本工具当作事务。
- 所有流事件必须版本连续；未知事件、解码/升级失败或取消均失败关闭。读取期间允许新事件出现，提交时仍以最终读取版本执行乐观锁检查。
- 不自动读取、写入或转换旧聚合快照；NewEventStream 拒绝 SnapshotManager。显式模型的快照需要独立、明确的格式设计和迁移。
- StateRepository 是有限提交工具，不是授权应用模板。业务用例负责权限、作用域及最终状态校验；事件元数据沿用已有 contextx 注入方式。

可运行示例：Runtime `examples/domain/explicit-eventsourced`，以业务 accountService 显式组合账户级授权，具名 Deposit/Withdraw 命令携带调用方 ExpectedVersion，拒绝过期命令后仍由追加检查并发。示例授权策略只允许固定演示账户；生产必须注入真实权限与租户边界。兼容性与故障隔离测试：`eventstore/state_repository_test.go`，同一实际内存事件流分别由传统聚合和显式模型回放。

事件历史是事件流视图，不是完整操作审计。ES 的 `EventHistory` / `ActionEventHistoryRead` 使用 `event_history_read`，旧 ES `AuditTrail` / `audit_read` 不保留别名；权限目录与 HTTP 路由同步迁移。CRUD 操作审计不受此命名变化影响。
