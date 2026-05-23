# messaging/command（命令语义层）

`messaging/command` 是基于 `messaging` 的 CQRS 命令层：提供 `Command` 载体、命令投递端口、命令执行端口，以及可同时复用于两侧的命令中间件。

它的定位需要和 `eventing` 区分开看：

- `messaging/command` 关心的是“命令如何被表达、投递、执行”；
- `eventing` 关心的是“事件如何作为事实被存储、发布、订阅、投影和重放”。

## 顶层概念

- `command.Command`：命令载体（嵌入 `messaging.Message`）
  - `Kind=command`
  - `Type=commandType`（命令名，例如 `CreateUser`）
  - `AggregateID/AggregateType`：聚合路由与并发控制所需字段
- `command.CommandBus`：命令投递端口，对 `messaging.IMessageBus` 的薄封装
  - `Dispatch(ctx, cmd)`：把命令发布到消息总线 / transport
  - `Use(middleware)`：注册投递侧中间件
- `command.ICommandExecutor`：显式命令执行端口
  - `Execute(ctx, cmd)`：返回真实业务执行结果
- `command.CommandExecutor`：本地命令执行器
  - `RegisterHandler(commandType, handler)`：注册本地 handler
  - `Execute(ctx, cmd)`：在当前调用栈内执行命令
  - `Use(middleware)`：注册执行侧中间件

## Metadata（中性立场）

`messaging/command` 不预设 `user_id/request_id/tenant_id/trace_id/...` 等元数据键，也不提供对应的 `WithXxx()` 便捷方法。

- `Command` 只保证命令语义与聚合字段（`AggregateID/AggregateType`）。
- 需要透传的业务信息由上层（装配/中间件）自行决定 key 与编码方式：`cmd.WithMetadata(key, value)`。

## 路由语义

命令名（`cmd.GetType()`）就是命令的逻辑路由键。

- 在 `CommandBus` 中，它决定消息发往哪个订阅者；
- 在 `CommandExecutor` 中，它决定命中哪个本地 handler。

## 中间件（messaging/command/middleware）

常见能力：

- 幂等：`IdempotencyMiddleware`（按命令 ID 去重，避免重复执行）
- 校验：`ValidationMiddleware`（对 payload 做校验）
- 租户：`TenantMiddleware`（将 tenant_id 注入 metadata）
- 聚合锁：`AggregateLockMiddleware`（同聚合串行执行，避免并发冲突放大）

这些中间件基于 `messaging.IMiddleware`，因此可以同时挂到 `CommandBus`（投递侧）或 `CommandExecutor`（执行侧），由组合根决定作用位置。

## 投递 vs 执行

- `Dispatch` 表达“命令投递”语义：是否成功进入消息总线 / transport
- `Execute` 表达“命令执行”语义：是否真实完成 handler 处理

对应用层的建议：

- 需要异步消息投递时，依赖 `CommandBus.Dispatch`
- 需要依赖真实业务结果时，依赖 `ICommandExecutor.Execute`
- 不要再用 `CommandBus` 承担本地 handler registry；本地执行请直接用 `CommandExecutor`

## 同步/异步注意事项

- `CommandBus` 是否同步，取决于底层 transport；`Dispatch` 的返回值不应被解释为“handler 一定已经执行完成”
- `CommandExecutor` 永远是本地同步执行，适合 saga / process / template method 等需要立即拿到结果的场景
- 如果业务是异步长流程，请用显式状态推进模型（例如 operation/workflow/result event），而不是假设 `Dispatch()` 能返回最终业务结果
