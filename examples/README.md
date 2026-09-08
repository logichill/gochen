# Core 示例

以下示例只依赖 Core 和标准库，在本仓库运行：

| 示例 | 内容 | 命令 |
| --- | --- | --- |
| [死信处理](messaging/deadletter/main.go) | 异步 handler 失败记录 | `GOWORK=off go run ./examples/messaging/deadletter` |
| [任务与策略](task/policy/main.go) | 任务监督、重试、限流、熔断 | `GOWORK=off go run ./examples/task/policy` |

SQL、REST、Host、Saga 与 Workflow 的配套示例位于 `gochen-runtime` 仓库 `examples/`，接入说明见[下游指南](../docs/guides/downstream-guide.md)。
