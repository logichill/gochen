# Saga 事件与生命周期

SagaOrchestrator 在执行过程中会通过 EventBus 发布携带 `saga_id` 的生命周期事件，便于观测与追踪。

## 事件类型

- `SagaStarted`
- `SagaStepCompleted`
- `SagaStepFailed`
- `SagaCompensationStarted`
- `SagaCompensationStepFailed`
- `SagaCompensated`
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
eventBus.SubscribeEvent(ctx, saga.EventSagaStepFailed, handler)
eventBus.SubscribeEvent(ctx, saga.EventSagaFailed, handler)
```

可在监控/日志中订阅失败类事件，或用于驱动告警。
