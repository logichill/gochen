# messaging：消息内核

`messaging` 提供消息信封、MessageBus、中间件、Transport 和处理失败记录。Core 内置同步 direct 与异步 memory Transport；外部消息队列由业务或扩展仓库适配。

## 基本契约

| 概念 | 语义 |
| --- | --- |
| `IMessage` | ID、Kind、Type、Payload、Metadata |
| Type | 具体消息名，是 Publish / Subscribe 的路由键 |
| Kind | command / event / query / unknown 类别，用于语义和观测 |
| Metadata | string-only 元数据，业务显式编码复杂值 |
| `IMessageBus` | 中间件链与消息发布、订阅 |
| `ITransport` | Publish、PublishAll、Subscribe、Start、Stop、Stats |

接口定义见 [transport.go](transport.go)。`messaging/command` 处理命令语义，`eventing/bus` 将总线收窄到事件；messaging 不依赖 eventing。

## 同步与异步

- `transport/direct` 在发布调用中执行 handler，处理错误可返回发布方。
- `transport/memory` 使用 worker，Publish 成功表示消息进入内存队列，handler 失败通过日志、hook 或 deadletter 记录。
- 可通过 `ISynchronousTransport` 探测同步能力。异步发布结果不能替代业务执行结果；需要同步命令结果时使用 [CommandExecutor](command/README.md)。

handler 的并发由 Transport 与发布侧共同决定，业务处理器应可并发调用并能处理重复消息。框架不统一承诺同一类型、聚合或全局消息的顺序。

## 异步消息快照

异步入队前冻结信封，避免调用方发布后修改 payload / metadata：

- 基础 Message 深拷贝常见可变载荷与元数据。
- 自定义 IMessage 实现 `IMessageEnvelopeCloner`，保留具体类型并负责克隆。
- 确实不可变的类型可声明 `IImmutableMessageEnvelope`。
- 未声明克隆或不可变契约的自定义类型被 memory Transport 拒绝。

## 上下文传播

MessageBus 将 context 中的 tenant / trace / operator 等语义写入 Metadata，消费时可从元数据补齐上下文。现有 ctx 优先，metadata 不覆盖已经存在的值；链路标识都缺失时可使用 message ID 补齐 trace ID。

元数据编码与信任来源由传输适配器和组合根明确，业务侧统一通过 `contextx` 访问这些语义。

## 停止与处理失败

`messaging.StopTransport(ctx, transport)` 统一处理 Stop、可选 StopWithSnapshot 及已停止的幂等语义。若调用方需要接收并重投 pending，应直接调用 `ITransportStopSnapshot.StopWithSnapshot` 保留其返回值。

正常停止等待已接受消息完成；超时快照可能包含排队中和处理中但未确认完成的消息，重投需按 ID 去重。

`messaging/deadletter.Entry` 记录消息、handler 类型、错误和发生时间，`ISink.Write` 接收失败快照，内存实现位于 `messaging/deadletter/memory`。它不承诺持久重试或自动重投；发布前的 Outbox 恢复见 [Outbox](../eventing/outbox/README.md)。

## 外部 Transport 接入

NATS、Redis Streams、Kafka 等适配器实现 ITransport，并明确以下语义：

1. Type 与主题 / stream 的映射、通配订阅、订阅取消与消费组行为。
2. 消息 ID、Kind、Type、载荷与 string Metadata 的编码规则。
3. 发布成功对应入队、broker ack 还是持久确认；handler 成功 / 失败与 ack / nack / 重投的对应关系。
4. ctx 取消、容量限制、错误上报和 Start / Stop 的资源释放边界。
5. 至少一次投递下的幂等、消息顺序和未完成消息恢复。

适配器应在自身仓库维护可编译实现与测试，不在 Core Markdown 中复制第三方驱动代码。RPC 风格的远程调用由业务单独建模。

## 并发与验证

MessageBus 可并发发布与订阅。Use / SetHandlerErrorHook 等配置在装配期完成，避免运行期时序改变中间件生效边界。

多消息 `PublishAll` 先逐条执行中间件，再统一投递。中间件必须同步调用 `next`，可按顺序调用多次；一次中间件调用返回错误或 panic 时，会撤销该次调用暂存的消息及幂等预留，保留此前成功准备的消息，供外层恢复或重试。即使调用返回 `nil`，若该次没有保留下来的待投递消息，也会撤销该次新增的幂等状态，使被过滤或撤销的命令仍可重试。

每次 `Publish` / `PublishAll` 调用的幂等作用域独立。嵌套发布继续继承租户、链路、取消信号和截止时间，已完成的内层投递不会随外层批次回滚。

Core 示例见[死信处理](../examples/messaging/deadletter/main.go)。在 Core 仓库执行 `GOWORK=off go test -count=1 ./messaging/...`；并发行为由各 Transport 与总线测试覆盖。
