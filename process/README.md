# process：过程运行时

`process` 提供多步骤推进、补偿、互斥执行和后台任务监督能力，业务逻辑由调用方定义。

| 包 | 职责 | 入口 |
| --- | --- | --- |
| [saga](saga/README.md) | 顺序执行步骤，失败时逆序补偿，保存进度以恢复 | `SagaOrchestrator` |
| [workflow](workflow/README.md) | 定义、实例、条件分支、汇聚、驳回与生命周期 | `Engine` |
| `lock` | 按业务 key 串行化，支持可替换锁提供者 | `ILockProvider` |
| `task` | 受控 goroutine、失败记录、取消与停止等待 | `TaskSupervisor` |

写入口结果和 accepted / processing / settled 状态由 [app/operation](../app/operation/README.md) 表达。重试、限流与熔断由 [policy](../policy/README.md) 提供，消息传递由 [messaging](../messaging/README.md) 承担。

Core 提供内存状态存储。生产持久化与跨进程并发控制由注入的 Store / LockProvider 负责，SQL 实现在 `gochen-runtime` 仓库 `process/`。事件或状态中的业务数据应使用可稳定序列化的快照，避免共享可变对象。

Saga 与 Workflow 示例位于 `gochen-runtime` 仓库 `examples/process/`。Core 任务示例见[任务与策略](../examples/task/policy/main.go)。
