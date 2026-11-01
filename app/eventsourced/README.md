# 事件溯源应用模板

`app/eventsourced` 提供领域仓储、事件溯源应用与审计轨迹，依赖领域端口。组合根按需选择以下独立适配包，核心模板不反向依赖它们：

| 包 | 内容 |
| --- | --- |
| `app/eventsourced/eventstore` | DomainEventStore、快照仓储装饰器、事件历史查询 |
| `app/eventsourced/subscriber` | 事件处理模板、投影模板、处理器/投影注册与清理 |

使用 Runtime 时，常规接入推荐 `quick.EventSourced` / `quick.AtomicEventSourced`，统一组合领域存储与仓储，聚合类型只声明一次。发布模式与依赖仍显式提供，示例见 [Quick 装配](../../docs/guides/quick-assembly.md#事件溯源)。仅依赖 Core 或需要自定义领域存储时使用下面的底层构造入口。

聚合通过 `domain/eventsourced.New` 或 `InitAggregate` 构造，处理器统一命名为 `ApplyXxx`。启动时注册并校验 metadata；命令方法负责校验，处理器仅进行确定性的内存状态更新。签名校验无法证明业务回放正确，接入时应执行[聚合使用指南](../../domain/eventsourced/README.md)中的回放验证。

单聚合使用 `NewEventSourcedRepository`，不暴露批量能力。原子批量使用 `eventstore.NewAtomicDomainEventStore` 和 `NewAtomicEventSourcedRepository`：构造阶段要求 `PublishModePersistOnly` 及底层 `store.IAtomicEventStore`。`IAtomicEventSourcedRepository.SaveAll` 成功后统一清除待提交事件，拒绝重复聚合 ID，不会循环单项保存来模拟原子性。

传给 `SaveAll` 的无待提交事件聚合会作为版本守卫保留，验证相关读取未在并发操作中变更。调用方仍需收集完整读取依赖；单独提交写集合不能防止业务判断基于过期的其他聚合。

保存返回 `contextx.AfterCommitError` 表示主保存已经完成、后续阶段失败。仓储仍会标记相应事件已提交，`Application` 同时返回已提交聚合与该错误，并将其排除在并发重试之外，即使配置了宽泛的自定义错误分类器。Operation 将其表达为 degraded 终态，调用方应核对结果或补偿失败阶段，不能直接重跑整个业务命令。

## 应用入口

`NewApplication(repo, Options)` 是事件溯源业务侧的唯一入口：

- `Get`：回放聚合并按 `read` 规约校验当前状态；
- `Execute`：修改已有聚合，保存冲突时按 `ConcurrencyRetry` 重试，命令函数必须可重入；
- `ExecuteNew`：新建聚合，要求回调产出的聚合 `GetExpectedVersion()` 为零，不做并发重试；
- `AuditTrail`：配置 `History` 后按 `audit_read` 规约读取事件历史。

授权器返回的规约在内存中对回放后的聚合求值：读入状态不匹配时 `Get` / `Execute` / `AuditTrail` 返回 NotFound。命令执行成功后依次做赋值核对、`domain.IValidatable` 与 `Options.Validator` 校验，再检查最终状态的写规约；`Execute` / `ExecuteNew` 的写规约不匹配时返回 Forbidden，并保持事件流原状。`ExecuteNew` 收到已有聚合时返回 InvalidInput。操作人、动作、原因与关联 ID 写入事件元数据，历史条目直接读取这些字段。命令总线接入使用 `AsExecuteHandler` / `AsExecuteNewHandler`；`Execute` 不提供跨聚合原子保证，多聚合写入使用原子仓储。

事件溯源仓储的 `Get` / `Save` 不接收 `spec.Spec`（与 SQL 仓储端口 D6 的签名不同）：事件流是唯一事实来源，不存在可合并进 `WHERE` 的行；规约由 Application 在回放后对聚合求值，判定失败时不追加事件。
