# Messaging（消息内核）

`messaging` 是 gochen 的“消息机制内核”：提供统一的消息模型、MessageBus、中间件与 Transport 抽象；不绑定具体基础设施（NATS/Redis/Kafka 等实现建议放业务仓库或独立 infra 仓库）。

## 顶层概念

- `messaging.IMessage`：消息最小公共视图
  - `ID`：唯一标识（幂等/诊断）
  - `Kind`：类别（`command`/`event`/`query`/`unknown`），用于语义判定与观测
  - `Type`：**具体消息名**（例如 `CreateUser` / `OrderCreated`），用于路由键/订阅键
  - `Payload`：载荷（任意）
  - `Metadata`：元数据（string-only）
- `messaging.IMessageBus`：消息总线（中间件链 + Publish/Subscribe）
- `messaging.ITransport`：传输层抽象（决定同步/异步语义）

## 关键约定

### 1) 路由键 = Type（不是 Kind）

- `Subscribe(ctx, messageType, handler)` 的 `messageType` 语义固定为 **Type（具体消息名）**。
- `Kind` 不承担路由职责；仅用于中间件/观测的“类别判定”。

### 2) Metadata 只接受 string

`messaging.Metadata` 内部为 `map[string]string`：

- 跨进程/跨 JSON 更稳定（避免 `float64/json.Number/int64` 等类型漂移）
- 约束更清晰：基础设施层不猜测业务类型，业务自行决定如何编码（例如 `"true"` / `"42"`）

### 3) 同步 vs 异步语义由 Transport 决定

- `messaging/transport/direct`：同步执行 handler；Publish 的 error 更接近“handler 的业务失败”
- `messaging/transport/memory`：异步 worker；Publish 的 error 仅保证“进入传输层”，handler 失败通过日志/hook/DLQ 收敛

> 需要在上层探测语义时，可使用可选能力接口：`messaging.ISynchronousTransport`。

### 4) 链路贯通（ctx vs metadata）

MessageBus 内置“默认贯通”逻辑，用于将 `metadata` 的链路语义与 `message.Metadata` 做双向补齐：

- Publish：确保 `metadata` 携带 `tenant_id/trace_id/operator`，跨进程可关联
- Consume：若 Transport 未透传上游 ctx，则可从 `metadata` 派生回 ctx

强约定的优先级规则（避免同进程链路被覆盖导致排障混乱）：

- `ctx` 优先：同进程场景不允许 `metadata` 覆盖 `ctx`
- `metadata` 仅用于“ctx 缺失时补齐”（典型：跨进程/异步 Transport 未透传 ctx）
- fallback：当二者都缺失时，使用 message.ID 兜底生成 trace_id

## 与 messaging/command/eventing 的关系

- `messaging/command`：命令语义层（`Command`/`CommandBus`/`CommandExecutor`/命令中间件），依赖 `messaging`
- `eventing`：事件基础设施层（EventStore/EventBus/Outbox/Projection），内部通过 `messaging` 进行传输与桥接

## Deadletter（异步错误收敛）

当底层 Transport 是异步语义（例如 `messaging/transport/memory`）时，handler 返回 error 往往无法传播给发布者；此时推荐使用 DLQ 收敛失败：

- 记录结构：`messaging/deadletter.Entry`（message + handlerType + error + timestamp）
- 写入抽象：`messaging/deadletter.Sink`（可实现为内存/SQL/外部队列）
- 参考实现：`messaging/deadletter/memory`

## 跨进程 / 消息队列

`messaging` 核心层不再内置独立的 bridge/client-server 抽象。

- 远端发送与订阅统一通过 `messaging.ITransport` / `messaging.IMessageBus` / `messaging.IMessageHandler` 建模。
- 若需要基于 NATS、Redis Streams、Kafka 等消息队列做跨进程通信，应在业务仓库或扩展层提供对应 `ITransport` 实现。
- 若需要 HTTP、gRPC 等 RPC 风格的远程调用，应在业务仓库单独建模，不再放入 messaging core。

## 参考实现

- 内置：`messaging/transport/memory`、`messaging/transport/direct`
- 文档级参考实现（复制到业务仓库使用）：
  - `messaging/transport/redisstreams/README.md`
  - `messaging/transport/natsjetstream/README.md`

## 并发与线程安全（契约）

- `messaging.MessageBus` 支持并发复用：`Publish/PublishAll/Subscribe` 与返回的 `UnsubscribeFunc` 可在多 goroutine 中安全调用。
- `Use`/`SetHandlerErrorHook` 虽然有锁保护，但推荐在“装配期”完成；运行期动态修改会带来非确定的中间件生效边界（不产生数据竞态，但行为取决于时序）。
- `Transport` 决定同步/异步语义，从而决定 `Publish` 的错误含义与 handler 并发度：
  - `transport/direct`：同步执行 handler；并发度主要由发布侧 goroutine 决定；
  - `transport/memory`：异步 worker；并发度由 worker 数与订阅分发策略决定。

相关竞态回归测试：`messaging/bus_race_test.go`。
