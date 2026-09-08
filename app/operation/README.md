# Operation：写操作结果与状态跟踪

`app/operation` 为应用写入口提供统一结果信封、可选状态存储、幂等协调、Tracker 和 SSE 输出。业务写入仍由回调执行，Runner 本身不调度后台任务。

## 执行模式

| 模式 | 成功默认状态 | 用途 |
| --- | --- | --- |
| `ModeInline` | `settled` | 返回时结果已收敛，不生成 operation ID |
| `ModeTracked` | `accepted` | 生成 operation ID，允许后续查询结果是否收敛 |

`Result.Operation` 保存 ID、Type、Mode、Status；业务数据位于 `Result.Result`，资源是 `*Resource{Type, ID}`。状态和 JSON 字段详见 [SPEC](SPEC.md)。

## 装配示例

下例演示 tracked 信封的生成与内存保存，业务写入应放在 handler 内：

```go
package main

import (
	"context"
	"fmt"
	"log"

	"gochen/app/operation"
)

func main() {
	store := operation.NewMemoryStore()
	runner := operation.NewRunner(&operation.RunnerOptions{Store: store})
	result, err := runner.Execute(context.Background(), &operation.Spec{
		Type:           "order.update",
		Mode:           operation.ModeTracked,
		Resource:       &operation.Resource{Type: "order", ID: "42"},
		IdempotencyKey: "order-update-42-request-1",
	}, func(ctx context.Context) (*operation.Result, error) {
		// 在这里调用应用服务；可用 OperationIDFromContext(ctx) 关联写入。
		return &operation.Result{Result: map[string]any{"queued": true}}, nil
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(result.Operation.ID, result.Operation.Status)
}
```

Runner 会同步调用 handler；tracked 表示需要跟踪结果，不会自动让 handler 异步执行。需要队列或工作流时由业务显式提交。

## 状态查询与 SSE

复用同一个 Store 创建 `NewTracker(&TrackerOptions{Store: store, Settlement: checker})`。`Tracker.Load` 读取状态并调用业务的 `ISettlementChecker`：尚未收敛时将 accepted 推进为 processing，收敛后保存 settled，终态直接返回。

HTTP handler 可组合 `PrepareStreamHeaders` 与 `Stream` 输出 SSE，也可直接调用 Tracker 实现轮询。路由、鉴权、操作归属检查和存储保留周期由接入方配置；Runner 的 URL builder 只生成链接，不注册路由。

`MemoryStore` 适合测试或单进程短期使用。需要重启后查询时注入持久化 `IStore`；幂等键还要求 `IIdempotencyStore`，多实例避免重复执行需支持预占接口，详见 [SPEC](SPEC.md#存储与幂等)。

## 应用层接入

- 普通 CRUD 或自定义写入口直接使用 `Runner.Execute(ctx, spec, handler)` 包装。
- `app/eventsourced.EventSourcedService` 提供 `ExecuteCommandWithOperation` 和 `ExecuteCommandWithResolvedOperation`；tracked 要求显式配置 runner，并由组合根为其配置可查询的 Store。
- 多步骤补偿和状态机推进使用 [process](../../process/README.md)。
