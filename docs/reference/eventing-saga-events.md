# Saga 生命周期事件

SagaOrchestrator 在配置 EventBus 时发布生命周期事件，事件聚合类型为 Saga。事件用于诊断、日志和告警，不与 Saga 状态保存共享事务；需要可靠业务事实时，由步骤侧使用 EventStore / Outbox。

## 事件类型

| 事件 | 含义 |
| --- | --- |
| SagaStarted / SagaResumed | 开始执行 / 恢复执行 |
| SagaStepCompleted / SagaStepFailed | 正向步骤成功 / 失败 |
| SagaCompensationStarted | 开始补偿 |
| SagaCompensationStepCompleted / SagaCompensationStepFailed | 单个补偿步骤成功 / 失败 |
| SagaCompensationCompleted | 补偿命令执行完成 |
| SagaCompleted | Saga 完成 |
| SagaCompletionFailed | 全部步骤成功但 OnComplete 失败，等待恢复完成回调 |
| SagaFailed | Saga 执行失败 |

枚举定义见 [process/saga/events.go](../../process/saga/events.go)。Resume 首先发布 SagaResumed，后续事件与正常执行路径采用相同语义。补偿命令已完成但状态保存失败时仍可出现 SagaCompensationCompleted；须结合返回错误和扩展字段判断持久化结果。

## 载荷

载荷包含 `saga_id`、`step`、`status`、`error`、`timestamp`、`extra`。Metadata 也包含 saga_id、status、step。extra 可携带步骤耗时等诊断信息，字段取决于事件阶段。

订阅时使用 `eventBus.SubscribeEvent(ctx, saga.EventSagaCompletionFailed.String(), handler)` 等标准 EventBus 入口，并保存返回的 UnsubscribeFunc，在模块停止时取消订阅。handler 使用 `bus.EventHandlerFunc` 或实现 IEventHandler，处理时遵循[事件总线并发语义](../../eventing/bus/README.md)。

执行与恢复规则见 [Saga](../../process/saga/README.md)。
