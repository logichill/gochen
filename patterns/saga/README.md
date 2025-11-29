# Saga Pattern Implementation

`patterns/saga` 提供了基于编排器（Orchestrator）模式的 Saga 分布式事务实现。它旨在解决微服务架构中跨多个服务的长时事务一致性问题。

## 核心设计原理

本实现遵循以下设计原则：

1.  **编排器模式 (Orchestration)**: 使用中心化的 `SagaOrchestrator` 来协调事务的执行，而不是通过服务间的事件编排（Choreography）。这使得流程逻辑更加集中和易于管理。
2.  **复用命令总线 (Command Bus)**: Saga 的每个步骤本质上是分发一个命令。我们复用了 `messaging/command` 模块，使得 Saga 步骤的定义与普通的命令处理保持一致。
3.  **自动补偿 (Automatic Compensation)**: 当某个步骤失败时，编排器会自动逆序执行已完成步骤的补偿命令，以回滚副作用。
4.  **状态持久化 (State Persistence)**: 支持保存 Saga 的执行状态，允许在进程重启后从断点恢复（Resume）执行。
5.  **可观测性 (Observability)**: 集成了 `eventing` 系统，Saga 的每个生命周期事件（开始、步骤完成、失败、补偿等）都会发布相应的领域事件。

## 核心组件

*   **ISaga**: 定义 Saga 的接口，包含获取 ID、获取步骤列表以及完成/失败回调。
*   **SagaStep**: 定义单个事务步骤，包含：
    *   `Command`: 正向操作的命令生成函数。
    *   `Compensation`: (可选) 补偿操作的命令生成函数。
    *   `OnSuccess`/`OnFailure`: 步骤级别的回调。
*   **SagaOrchestrator**: 核心执行引擎，负责按顺序执行步骤，处理错误并触发补偿流程。
*   **ISagaStateStore**: 状态存储接口，用于持久化 Saga 的运行状态。

## 使用指南

### 1. 定义 Saga

创建一个结构体实现 `ISaga` 接口。你可以嵌入 `BaseSaga` 来使用默认的回调实现。

```go
import (
    "context"
    "gochen/messaging/command"
    "gochen/patterns/saga"
)

type CreateOrderSaga struct {
    saga.BaseSaga
    OrderID string
    Amount  int64
}

func (s *CreateOrderSaga) GetID() string {
    return s.OrderID
}

func (s *CreateOrderSaga) GetSteps() []*saga.SagaStep {
    return []*saga.SagaStep{
        // 步骤 1: 预扣库存
        saga.NewSagaStep("ReserveInventory", func(ctx context.Context) (*command.Command, error) {
            return command.NewCommand(
                command.NewID(), 
                "reserve-inventory", 
                s.Amount, 
                "Inventory", 
                &ReserveInventoryCmd{OrderID: s.OrderID}
            ), nil
        }).WithCompensation(func(ctx context.Context) (*command.Command, error) {
            // 补偿: 释放库存
            return command.NewCommand(
                command.NewID(), 
                "release-inventory", 
                s.Amount, 
                "Inventory", 
                &ReleaseInventoryCmd{OrderID: s.OrderID}
            ), nil
        }),

        // 步骤 2: 创建订单
        saga.NewSagaStep("CreateOrder", func(ctx context.Context) (*command.Command, error) {
            return command.NewCommand(
                command.NewID(), 
                "create-order", 
                0, 
                "Order", 
                &CreateOrderCmd{OrderID: s.OrderID}
            ), nil
        }),
        // 此步骤如果是最后一步，可能不需要补偿，或者根据业务需求添加取消订单的补偿
    }
}
```

### 2. 初始化编排器

```go
// 假设你已经有了 commandBus 和 eventBus
stateStore := saga.NewMemorySagaStateStore() // 或使用 SQL 实现
orchestrator := saga.NewSagaOrchestrator(commandBus, eventBus, stateStore)
```

### 3. 执行 Saga

```go
orderSaga := &CreateOrderSaga{
    OrderID: "order-123",
    Amount:  100,
}

err := orchestrator.Execute(context.Background(), orderSaga)
if err != nil {
    // Saga 执行失败（已尝试自动补偿）
    log.Printf("Saga failed: %v", err)
}
```

## 错误处理与恢复

*   **同步执行**: `Execute` 方法是同步阻塞的，直到 Saga 完成或失败（包括补偿完成）。
*   **错误语义**: 如果 `Execute` 返回错误，通常意味着 Saga 最终失败了。错误对象中包含了原始错误和补偿过程中的错误（如果有）。
*   **恢复执行**: 如果使用了持久化的 `StateStore`，在系统崩溃重启后，可以加载未完成的 Saga 状态，并调用 `orchestrator.Resume(ctx, saga, state)` 继续执行。

## 注意事项

1.  **命令总线传输**: 为了确保 Saga 能正确感知步骤的失败，底层的 `CommandBus` 最好配置为使用 **同步传输 (Sync Transport)**。如果使用异步传输（如消息队列），`Dispatch` 方法可能只表示消息已发送，无法立即得知业务处理结果，这会导致 Saga 误判步骤成功。
2.  **幂等性**: 由于网络重试或恢复执行，Saga 的步骤命令和补偿命令必须设计为幂等的。
