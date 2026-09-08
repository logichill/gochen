# Saga

`process/saga` 按顺序执行步骤，失败时对已完成且配置了补偿的步骤逆序执行补偿命令。执行与补偿结果都来自同步 `command.ICommandExecutor`。

## 定义与装配

| 入口 | 职责 |
| --- | --- |
| `ISaga` | ID、Steps、OnComplete、OnFailed |
| `BaseSaga` | 为可选业务回调提供空实现 |
| `SagaStep` / `NewSagaStep` | 正向命令、可选补偿、步骤成功 / 失败回调 |
| `SagaOrchestrator` | Execute / Resume、状态保存、补偿与事件发布 |
| `ISagaStateStore` | 保存实例进度，内存实现为 `NewMemorySagaStateStore` |
| `WithLockProvider` | 配置同一 saga ID 的互斥执行 |

`NewSagaOrchestrator(commandExecutor, eventBus, stateStore, eventIDGenerator, logger)` 返回实例和 error；ID generator 与 logger 必须非空，eventBus 与 stateStore 可选。执行前须配置能够处理步骤命令的 CommandExecutor。

同一 Saga 内步骤名称非空且唯一，步骤和 Command 生成函数不能为 nil。命令 ID 由业务明确生成，恢复时步骤定义须保持一致。

## 执行与恢复

- Execute 创建初始状态；Store.Save 对重复 saga ID 返回冲突，不能覆盖已有进度。
- 步骤成功后保存进度；持久化失败中止执行并返回错误。
- 步骤失败触发已完成步骤的补偿；补偿无法撤回任意外部副作用，业务需定义对应动作。
- Resume 校验 SagaID、CurrentStep 与 CompletedSteps，发布 SagaResumed 后继续正常生命周期。
- compensating 中间态不能直接 Resume，须由业务安排补偿恢复。
- 全部步骤成功但 OnComplete 失败时进入 pending_completion，并发布 SagaCompletionFailed；Resume 重试完成回调。

步骤、补偿和 OnComplete 都应幂等。框架保存成功完成回调的标记以避免恢复路径重复执行，但回调与持久化之间仍需考虑故障。

## 并发与消息语义

默认不为同一 saga ID 加锁。并发或多实例调度须注入合适的 `lock.ILockProvider` 或由外部调度串行化；租约失效时中止后续推进。

异步 Transport 的 Dispatch 只表示投递，不能为 Saga 提供即时成败判断。需要异步多步骤过程时，由业务通过状态或结果事件驱动后续推进。

EventBus 发布的 Saga 生命周期事件用于观测，不与状态持久化共享事务。可靠业务事件由步骤侧的事件 Store / Outbox 承载，详见 [Saga 事件](../../docs/reference/eventing-saga-events.md)。

## 示例

在 `gochen-runtime` 仓库执行：

```bash
GOWORK=off go run ./examples/process/saga/basic
```

Core 单元测试位于本包，可运行 `GOWORK=off go test -count=1 ./process/saga`。
