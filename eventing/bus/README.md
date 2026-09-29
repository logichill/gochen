# EventBus

`eventing/bus` 在 `messaging.IMessageBus` 上提供事件语义，复用底层 Transport 与中间件。

## 入口

- `NewEventBus(messageBus)` 创建默认 EventBus。
- `IEventBus.PublishEvent` / `PublishEvents` 发布事件。
- `SubscribeEvent` 按 event type 订阅，`"*"` 表示全部事件。
- `SubscribeHandler` 根据 IEventHandler 声明的事件类型注册。
- 订阅返回 UnsubscribeFunc，用于生命周期收尾。

`SubscribeHandler` 的释放函数支持并发调用：逆序尝试全部订阅，聚合失败原因，后续只重试失败项；等待其他释放调用时响应 context 取消。注册中途失败会自动回滚，回滚失败时同时返回非 nil 释放函数与错误，调用方必须保留函数并用新的清理上下文重试。

`app/eventsourced.EventSourcedAutoRegistrar.RegisterHandlers` 遵循相同规则：批次失败后返回尚未清理的释放函数及注册、回滚错误；通过 `UnregisterHandlers` 继续清理。

## 并发、顺序与错误

EventBus 的并发与投递语义取决于底层 MessageBus / Transport。同步 direct 在发布调用中执行 handler；并发发布仍可能并发调用同一 handler。异步 memory 使用 worker，发布成功表示接受消息，处理失败通过日志、hook 或 deadletter 收敛。

处理器必须线程安全并满足幂等要求。框架不承诺全局或同聚合严格顺序；需要顺序时由组合根选择合适 Transport，并在消费侧按业务 key 串行化。

消息快照、停止与外部队列接入规则集中见 [messaging](../../messaging/README.md)。
