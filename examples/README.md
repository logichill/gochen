# Core 示例

以下示例只依赖 Core 和标准库，在本仓库运行：

| 示例 | 内容 | 命令 |
| --- | --- | --- |
| [强类型授权声明](auth/declaration/main.go) | 纯 Core 声明编译、Application 范围执行与独立命令注册源核对 | `GOWORK=off go run ./examples/auth/declaration` |
| [死信处理](messaging/deadletter/main.go) | 异步 handler 失败记录 | `GOWORK=off go run ./examples/messaging/deadletter` |
| [事件溯源装配与恢复](eventsourced/assembly/main.go) | 原子保存、Direct / Outbox、结果补存、快照恢复 | `GOWORK=off go run ./examples/eventsourced/assembly` |
| [任务与策略](task/policy/main.go) | 任务监督、重试、限流、熔断 | `GOWORK=off go run ./examples/task/policy` |

SQL、REST、Host、Saga 与 Workflow 的配套示例位于 `gochen-runtime` 仓库 `examples/`，接入说明见[下游指南](../docs/guides/downstream-guide.md)。
