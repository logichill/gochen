# Saga 事件与生命周期

SagaOrchestrator 在执行过程中会通过 EventBus 发布携带 `saga_id` 的生命周期事件，便于观测与追踪。

`Resume(ctx, saga, state)` 会先发布 `SagaResumed`，随后继续沿用和 `Execute(...)` 相同的步骤成功/失败、补偿完成、Saga 完成/失败事件语义。

当补偿命令已经全部执行完成，但补偿后的状态持久化失败时，事件流仍保留 `SagaCompensationCompleted` 语义；此时持久化故障会通过返回错误和事件扩展字段暴露，而不会被误报成 `SagaFailed`。

## 事件类型

- `SagaStarted`
- `SagaResumed`
- `SagaStepCompleted`
- `SagaStepFailed`
- `SagaCompensationStarted`
- `SagaCompensationStepCompleted`
- `SagaCompensationStepFailed`
- `SagaCompensationCompleted`
- `SagaCompleted`
- `SagaFailed`

## Payload 结构

```json
{
  "saga_id": "saga-123",
  "step": "ReserveInventory",
  "status": "SagaStepCompleted",
  "error": "",
  "timestamp": "2025-11-20T23:23:00Z",
  "extra": {
    "step": "ReserveInventory",
    "duration_ms": 12
  }
}
```

元数据（Metadata）也包含 `saga_id`、`status`、`step` 便于订阅端过滤。

## 订阅示例

```go
handler := bus.EventHandlerFunc(func(ctx context.Context, evt eventing.IEvent) error {
	// TODO: 监控/告警/日志
	return nil
})

unsub1, err := eventBus.SubscribeEvent(ctx, saga.EventSagaStepFailed.String(), handler)
if err != nil {
	// 推荐在装配期 fail-fast（返回错误 / panic / 退出进程）
	return err
}
unsub2, err := eventBus.SubscribeEvent(ctx, saga.EventSagaFailed.String(), handler)
if err != nil {
	_ = unsub1(ctx)
	return err
}
defer func() { _ = unsub2(ctx) }()
defer func() { _ = unsub1(ctx) }()
```

可在监控/日志中订阅失败类事件，或用于驱动告警。
