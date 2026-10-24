# 事件溯源应用模板

`app/eventsourced` 提供领域仓储、命令执行、重试及 Operation 接入，依赖领域端口。组合根按需选择以下独立适配包，核心模板不反向依赖它们：

| 包 | 内容 |
| --- | --- |
| `app/eventsourced/eventstore` | DomainEventStore、快照仓储装饰器、事件历史查询 |
| `app/eventsourced/subscriber` | 事件处理模板、投影模板、处理器/投影注册与清理 |

使用 Runtime 时，常规接入推荐 `quick.EventSourced` / `quick.AtomicEventSourced`，统一组合领域存储与仓储，聚合类型只声明一次。发布模式与依赖仍显式提供，示例见 [Quick 装配](../../docs/guides/quick-assembly.md#事件溯源)。仅依赖 Core 或需要自定义领域存储时使用下面的底层构造入口。

聚合通过 `domain/eventsourced.New` 或 `InitAggregate` 构造，处理器统一命名为 `ApplyXxx`。启动时注册并校验 metadata；命令方法负责校验，处理器仅进行确定性的内存状态更新。签名校验无法证明业务回放正确，接入时应执行[聚合使用指南](../../domain/eventsourced/README.md)中的回放验证。

单聚合使用 `NewEventSourcedRepository`，不暴露批量能力。原子批量使用 `eventstore.NewAtomicDomainEventStore` 和 `NewAtomicEventSourcedRepository`：构造阶段要求 `PublishModePersistOnly` 及底层 `store.IAtomicEventStore`。`IAtomicEventSourcedRepository.SaveAll` 成功后统一清除待提交事件，拒绝重复聚合 ID，不会循环单项保存来模拟原子性。

传给 `SaveAll` 的无待提交事件聚合会作为版本守卫保留，验证相关读取未在并发操作中变更。调用方仍需收集完整读取依赖；单独提交写集合不能防止业务判断基于过期的其他聚合。

保存返回 `contextx.AfterCommitError` 表示主保存已经完成、后续阶段失败。仓储仍会标记相应事件已提交，命令服务不会将该错误纳入并发重试，即使配置了宽泛的自定义错误分类器。Operation 将其表达为 degraded 终态，调用方应核对结果或补偿失败阶段，不能直接重跑整个业务命令。
