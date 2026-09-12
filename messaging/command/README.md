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

`IdempotencyMiddleware` 的批量去重锁保持到实际投递完成，只有整批成功后才记录已处理。单条和批量处理争用同一幂等键时均立即返回 `errors.Concurrency`，调用方可重试当前操作，批量发布应重试整个批次。批量准备期间，锁争用或中间件返回错误、panic 会回滚对应尝试的暂存消息、命令预留和提交回调，并释放锁；返回 `nil` 但没有保留待投递消息的调用也会撤销本次新增的幂等状态，允许在同一批次作用域重试。多个中间件实例的批量状态相互隔离。

每次发布调用拥有独立的幂等作用域：已成功的嵌套投递不受外层失败影响；嵌套调用争用外层仍持有的同一幂等键时，也返回 `errors.Concurrency`。

Command 自身不定义身份模型，额外元数据使用 WithMetadata 显式编码。上下文传播遵循 [messaging](../README.md#上下文传播)，身份与链路语义通过 contextx 访问。

## 选择边界

需要异步投递时调用 CommandBus.Dispatch；需要即时成败、Saga 补偿判断或本地业务返回值时调用 ICommandExecutor.Execute。Dispatch 是否同步由 Transport 决定，不能统一解释为 handler 已完成。

长流程结果由业务状态、[Operation](../../app/operation/README.md)或 [Workflow](../../process/workflow/README.md)表达。事件事实的保存与投影见 [eventing](../../eventing/README.md)。
