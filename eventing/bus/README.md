# eventing/bus：事件总线（EventBus）

`eventing/bus` 是对 `messaging.IMessageBus` 的薄包装：把 `messaging.IMessage` 收敛为 `eventing.IEvent` 语义，并提供按事件类型订阅/批量订阅的便捷方法。

## 顶层概念

- `bus.IEventBus`：事件总线接口（`PublishEvent/PublishEvents/SubscribeEvent/SubscribeHandler`）。
- `bus.EventBus`：默认实现（内部直接复用一个 `messaging.IMessageBus`）。
- `bus.IEventHandler`：事件处理器接口（声明事件类型列表 + `HandleEvent`）。

## 并发与顺序（契约）

### 1) EventBus 实例可并发复用

`EventBus` 自身不维护业务状态；并发安全主要由底层 `messaging.IMessageBus` 决定。只要底层 `IMessageBus` 是并发安全的，则：

- `PublishEvent/PublishEvents/SubscribeEvent/SubscribeHandler` 可在多 goroutine 中安全调用；
- `Subscribe*` 返回的 `UnsubscribeFunc` 可并发调用且幂等（内部使用 `sync.Once`）。

### 2) handler 可能并发执行

是否会并发执行 handler，取决于底层 `Transport` 的同步/异步语义与发布侧并发度：

- 同步 transport（例如 `messaging/transport/direct`）：一次 `Publish` 调用中通常按订阅顺序依次调用 handler；但如果发布者并发调用 `Publish`，handler 仍可能并发执行。
- 异步 transport（例如 `messaging/transport/memory`）：worker 池会并行调度 handler，默认就可能并发执行。

因此 **EventHandler 必须线程安全**（或在 handler 内部自行串行化/加锁），并且应按“至少一次投递”假设实现幂等。

### 3) 顺序语义不做强保证

框架不对“同一事件类型/同一聚合/全局事件流”的处理顺序做强保证：

- 需要严格顺序时，应在组合根选择单线程/同步语义 transport，并避免并发发布；或在消费侧引入基于 key 的串行化策略。

## 错误语义

- `PublishEvent` 的错误语义由底层 transport 决定：
  - 同步 transport：更接近 handler 的执行结果；
  - 异步 transport：更接近“消息被接受/入队”，handler 失败应通过日志/hook/DLQ 收敛。

更多细节见 `messaging/README.md` 的“同步 vs 异步语义”与“并发与线程安全（契约）”小节。

