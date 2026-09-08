# eventing：事件基础设施

`eventing` 提供事件模型、EventStore、EventBus、Outbox、Projection、轮询订阅与监控契约。Core 包含内存实现，SQL 持久化和 HTTP 监控适配在 `gochen-runtime` 仓库 `eventing/`。

## 能力导航

| 包 | 职责 |
| --- | --- |
| 根包 | `Event[ID]`、`IEvent`、`IStorableEvent` 与事件构造 |
| [store](store/README.md) | 追加、回放、事件流、快照与缓存 |
| [bus](bus/README.md) | 基于 MessageBus 的事件发布和订阅 |
| [outbox](outbox/README.md) | 事件与待发布记录原子写入、发布重试与死信 |
| [projection](projection/README.md) | 读模型、checkpoint、恢复与重建 |
| `registry` / `upcast` | 类型注册、载荷升级和强类型还原 |
| `subscription` | 从事件流轮询消费，可配置持久游标 |
| `monitoring` | 健康检查、指标、快照及路由模型 |

## 事件与消息

`IEvent` 扩展 `messaging.IMessage`，`Event[ID]` 复用消息信封。`eventing` 依赖 `messaging` 完成投递，后者不反向依赖事件层。外部队列接入 `messaging.ITransport`，EventBus 复用同一通道。

事件 ID 通过 `NewEvent(generator, ...)` 显式生成，或由 `NewEventWithID(...)` 传入。聚合身份由 aggregate type 与 aggregate ID 共同定义，事件版本与 payload schema version 分别表达聚合顺序和载荷结构。

## 消费边界

组合根创建并注入 Registry 与 UpgraderRegistry。消费载荷使用 `upcast.HydrateEventPayload` 或 `DecodeEventPayload[T]`，统一读取事件的 `EventSchemaVersion()`，完成升级和强类型还原。

`subscription` 在处理成功后推进游标。配置 CursorStore 与 Name 可保存消费位置；配置重试上限与跳过策略时，只有 DeadLetterFunc 成功才跳过失败事件，死信回调失败则停止推进。

Outbox、事件重放与异步传输要求消费者幂等。投影的事务与恢复边界见 [Projection](projection/README.md)。

## 监控

Core `monitoring.NewRegistry` 聚合 provider，`Routes` / `HandleRoute` 生成路由描述和响应内容。组合根可显式注入 registry。

Runtime `gochen-runtime/eventing/monitoring` 的包名为 `monitoringhttp`：`NewHandler` 导出 healthz / readyz，`NewHandlerWithRoutes` 可选择 FullRouteSet。metrics / snapshot 需由组合根明确启用并控制访问。

## 示例

完整链路见[事件溯源速查](../docs/reference/ddd-eventsourcing-quick-reference.md)。可运行 SQL 示例位于 `gochen-runtime` 仓库 `examples/domain/eventsourced`、`examples/infra/outbox/sql`、`examples/infra/projection/sql_checkpoint`。
