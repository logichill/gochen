# Eventing（事件基础设施）

`eventing` 提供 Event Sourcing / CQRS 的事件基础设施：事件模型、事件存储（EventStore）、事件总线（EventBus）、Outbox、投影（Projection）、轮询订阅（Subscription）与监控导出（Monitoring）。

## 你需要记住的几个点

- **Store 是事实来源**：事件写入与按聚合重建由 `eventing/store` 定义与承载。
- **Bus 负责投递**：复用 `messaging` 的 Publish/Subscribe/中间件机制，把参数类型收敛为 `eventing.IEvent`。
- **Outbox 保证可靠发布**：同事务写事件 + Outbox，事务外异步发布到 EventBus。
- **Projection 负责读模型**：把全局事件流转换为读模型；可选 checkpoint 支持重启恢复与追赶。
- **Subscription 是“没有消息总线时”的最小消费**：直接从全局事件流轮询读取并回调处理。
- **Monitoring 提供统一导出端点**：聚合健康/指标/快照并以 HTTP handler 方式导出。

## eventing 与 messaging 的关系

这两个包不是并列重复实现，而是**分层协作**：

- `messaging` 是消息机制内核：message envelope、MessageBus、Transport、中间件、DLQ。
- `eventing` 是事件事实链路：事件模型、EventStore、Outbox、Projection、Subscription、Monitoring。
- 设计上明确采用 **event is a specialized message**：`eventing.IEvent` 继承 `messaging.IMessage`，`eventing.Event[ID]` 复用 `messaging.Message` 作为信封层。
- 依赖方向是单向的：`eventing` 可以复用 `messaging` 做投递与传输，`messaging` 不反向依赖 `eventing`。
- 因此，NATS / Redis / Kafka 等异步通道应实现为 `messaging.ITransport`；`eventing` 通过 `eventing/bus` 走同一套消息基础设施，而不是再维护一套独立事件传输协议。

## 入口（建议阅读顺序）

- EventStore：`eventing/store/README.md`
- Outbox：`eventing/outbox/README.md`
- Projection：`eventing/projection/README.md`
- Payload 升级与 hydration：`eventing/upcast`
- Store 装饰器（tenant/tracing）：`eventing/store/decorators`

## eventing 根包（最小核心）

`gochen/eventing` 根包只保留事件模型与最小接口：

- `eventing.Event[ID]` / `eventing.IEvent` / `eventing.IStorableEvent`
- `eventing.NewEvent(...)`

其余能力（registry/upcast/decorators/monitoring/outbox/projection/store/...）都在子包中作为明确依赖引入，避免根包导入即带来大量概念与 API 表面积。

## EventBus（事件语义层）

`eventing/bus` 是对 `messaging.IMessageBus` 的“事件语义”包装：

- `bus.NewEventBus(messageBus)`：组装 `IEventBus`
- `SubscribeEvent(ctx, eventType, handler)`：按事件类型订阅；`eventType="*"` 订阅全部
- 取消订阅：返回 `messaging.UnsubscribeFunc`（绑定订阅实例，推荐 `defer` 回收）
- 它的职责是**收敛事件语义**，不是把 `eventing` 与 `messaging` 完全隔离成两套平行契约

## Upcast / Hydration（事件消费边界）

消费侧不要重复手写 `PayloadValue -> map/json -> UpgradeEventData -> DeserializeFromMap`。统一使用 `eventing/upcast`：

- `upcast.HydrateEventPayload(reg, upgraders, evt)`：返回升级并反序列化后的强类型 payload。
- `upcast.DecodeEventPayload[T](reg, upgraders, evt)`：在 hydration 后校验并返回目标类型。

该入口会读取事件自身的 `EventSchemaVersion()`，避免消费侧错误回退到 schema version 1。

## Subscription（轮询订阅）

`eventing/subscription` 适用于不引入异步消息总线/传输时的最小消费模型：

- at-least-once：handler 返回 error 时停止并返回错误
- 不包含持久化 checkpoint：调用方可通过 `Cursor()` 获取游标并自行持久化

## Monitoring（监控导出）

`eventing/monitoring` 提供框架侧统一导出端点：

- `reg, err := monitoring.NewRegistry()` + `monitoring.SetDefaultRegistry(reg)`：注册全局默认 registry
- 组合根/示例建议用 `must(err)` / `log.Fatal(err)` 显式快速失败（库层不再提供 `Must*` 版本）
- `monitoring.NewHTTPHandler(reg)`：导出 `/healthz`、`/metrics`、`/snapshot`
- 可选汇总 Outbox/Snapshot/Cache 等统计信息到同一端点（以 provider 的方式注入）

## 参考示例

- 事件溯源（领域视角）：`examples/domain/eventsourced`、`examples/domain/eventsourced_stringid`
- Outbox（SQL）：`examples/infra/outbox/sql`
- Projection：`examples/infra/projection/*`
