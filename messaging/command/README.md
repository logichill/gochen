# Command：命令投递与执行

`messaging/command` 在消息内核上定义写意图、投递端口、本地执行端口和命令中间件。

## 入口

| 类型 | 语义 |
| --- | --- |
| `Command` | 嵌入 Message，Kind 为 command，Type 为命令名，另含 AggregateID / AggregateType |
| `CommandBus` | 通过 Dispatch 投递到 MessageBus / Transport |
| `ICommandExecutor` | 通过 Execute 获取同步业务执行结果 |
| `CommandExecutor` | 本地 handler 注册表，RegisterHandler 后在当前调用栈 Execute |

命令名是路由键。CommandBus 与 CommandExecutor 都支持 Use 注册消息中间件；前者作用于投递边界，后者作用于执行边界。

## 中间件与元数据

`messaging/command/middleware` 提供：

- IdempotencyMiddleware：按命令 ID 去重。
- ValidationMiddleware：校验 payload。
- TenantMiddleware：传播租户元数据。
- AggregateLockMiddleware：同聚合串行执行。

Command 自身不定义身份模型，额外元数据使用 WithMetadata 显式编码。上下文传播遵循 [messaging](../README.md#上下文传播)，身份与链路语义通过 contextx 访问。

## 选择边界

需要异步投递时调用 CommandBus.Dispatch；需要即时成败、Saga 补偿判断或本地业务返回值时调用 ICommandExecutor.Execute。Dispatch 是否同步由 Transport 决定，不能统一解释为 handler 已完成。

长流程结果由业务状态、[Operation](../../app/operation/README.md)或 [Workflow](../../process/workflow/README.md)表达。事件事实的保存与投影见 [eventing](../../eventing/README.md)。
