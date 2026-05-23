# Saga（Orchestrator）

`process/saga` 提供基于“编排器（Orchestrator）”模型的 Saga 实现，用于组织跨多个步骤的长事务：按顺序执行步骤，失败时自动按逆序执行补偿。

## 顶层概念

| 概念 | 责任 | 典型使用者 |
|---|---|---|
| `ISaga` | 提供 `ID()` 与 `Steps()`；可选回调 `OnComplete/OnFailed`（可嵌入 `BaseSaga`） | 业务编排定义 |
| `SagaStep` | 一个步骤：`Command(ctx)` + 可选 `Compensation(ctx)`，以及步骤级回调 | 业务编排定义 |
| `SagaOrchestrator` | 执行引擎：`Execute/Resume`、错误处理、补偿、事件发布 | 应用装配/运行时 |
| `ISagaStateStore` | 状态持久化（可选）：保存进度用于重启恢复 | 基础设施层 |
| `lock.ILockProvider` | 可选并发控制：保证同一 `sagaID` 的 `Execute/Resume` 串行 | 基础设施层 |

## 执行语义（你需要知道的最少规则）
- **步骤是命令**：每个 `SagaStep` 通过 `command.ICommandExecutor` 执行一个命令；失败时执行补偿命令。
- **步骤定义必须稳定**：`SagaStep` 必须非 nil、名称非空且在同一 Saga 内唯一，`Command` 生成函数不能为空。
- **自动补偿**：任一步骤失败，编排器会对“已完成的步骤”按逆序执行补偿（若该步骤定义了补偿命令）。
- **状态持久化（可选）**：配置 `ISagaStateStore` 后会在关键节点 `Save/Update`；持久化失败视为严重一致性错误，会直接中止返回。
- **初始状态创建**：`ISagaStateStore.Save` 语义是“创建初始状态”；同一 `sagaID` 已存在时应返回冲突，而不是覆盖既有进度。
- **恢复执行**：进程重启后可读取持久化状态并调用 `Resume(ctx, saga, state)` 从 `CurrentStep` 继续。
- **恢复前校验**：`Resume` 会校验 `state.SagaID`、`CurrentStep` 与 `CompletedSteps` 必须和当前 Saga 定义一致；`compensating` 中间态不能直接恢复，需要人工或专门的补偿恢复流程处理。
- **恢复事件语义**：`Resume` 除了先发布 `EventSagaResumed` 外，后续步骤成功/失败、补偿完成、Saga 完成/失败事件与正常 `Execute` 路径保持一致。
- **可观测性**：若注入了 `eventing/bus.IEventBus`，编排器会发布 Saga 生命周期事件（`EventSagaStarted/.../EventSagaFailed`），事件载荷为 `eventing.Event`（`AggregateType="Saga"`）。

## 与 Command / Transport 语义的关系
Saga 依赖的是“命令是否执行失败”的信号，而不是“命令是否已经进入传输层”。

因此：

- `process/saga` 只依赖 `command.ICommandExecutor`
- 默认推荐直接装配 `command.CommandExecutor`
- 异步 Transport（memory/redisstreams/natsjetstream 等）只能表达“消息已投递”，不应直接拿来驱动需要即时补偿判断的 Saga

如果业务确实需要异步长流程，请改用显式状态推进模型（例如 operation/workflow/result event），而不是假设 `Dispatch()` 能返回最终业务结果。

## 最小示例

定义一个 Saga（示例仅展示形状；命令 ID/聚合类型按你的项目约定生成）：

```go
package orders

import (
	"context"

	"gochen/messaging/command"
	"gochen/process/saga"
)

type CreateOrderSaga struct {
	saga.BaseSaga
	OrderID string
}

func (s *CreateOrderSaga) ID() string { return s.OrderID }

func (s *CreateOrderSaga) Steps() []*saga.SagaStep {
	return []*saga.SagaStep{
		saga.NewSagaStep("ReserveInventory", func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("cmd-1", "ReserveInventory", s.OrderID, "Inventory", &ReserveInventory{OrderID: s.OrderID}), nil
		}).WithCompensation(func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("cmd-2", "ReleaseInventory", s.OrderID, "Inventory", &ReleaseInventory{OrderID: s.OrderID}), nil
		}),
		saga.NewSagaStep("CreateOrder", func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("cmd-3", "CreateOrder", s.OrderID, "Order", &CreateOrder{OrderID: s.OrderID}), nil
		}),
	}
}
```

装配并执行：

```go
import (
	"context"

	"gochen/eventing/bus"
	"gochen/messaging/command"
	"gochen/process/saga"
)

func run(ctx context.Context, evtBus bus.IEventBus) error {
	stateStore := saga.NewMemorySagaStateStore() // 生产环境可替换为持久化实现
	cmdExecutor := command.NewCommandExecutor()

	if err := cmdExecutor.RegisterHandler("ReserveInventory", reserveInventoryHandler); err != nil {
		return err
	}
	if err := cmdExecutor.RegisterHandler("ReleaseInventory", releaseInventoryHandler); err != nil {
		return err
	}
	if err := cmdExecutor.RegisterHandler("CreateOrder", createOrderHandler); err != nil {
		return err
	}

	orchestrator := saga.NewSagaOrchestrator(cmdExecutor, evtBus, stateStore)
	return orchestrator.Execute(ctx, &CreateOrderSaga{OrderID: "order-123"})
}
```

## 并发与幂等
- **同一 `sagaID` 必须串行**：默认不对同一 `sagaID` 做加锁；多实例/多协程调度同一 `sagaID` 时，建议注入 `lock.ILockProvider` 或在外部队列化。
- **步骤/补偿必须幂等**：至少一次投递 + 恢复执行都会带来重复执行的可能性。
