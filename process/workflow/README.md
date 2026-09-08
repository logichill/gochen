# Workflow

`process/workflow` 提供流程定义、实例状态、条件分支、汇聚、驳回、挂起 / 恢复 / 终止、超时检测与节点钩子。引擎管理流程位置，节点业务逻辑由调用方编排。

## 模型与存储

| 类型 | 职责 |
| --- | --- |
| `Definition` | DAG、递增版本、起始节点 |
| `Node` / `Edge` | 节点、出边、条件、默认分支、驳回目标与超时 |
| `State` | 固化定义版本、活动节点、等待汇聚、轨迹和业务数据 |
| `IStore` | 定义和实例存储 |
| `IOptimisticStore` | 按实例版本进行并发保存 |
| `IQueryableStore` | 枚举实例，供后台巡检发现目标 |
| `IConditionEvaluator` / `IConditionValidator` | 条件求值与可选的定义期语法校验 |

Core MemoryStore 适合测试和单进程场景。生产与多实例存储必须提供乐观并发控制，进程内 keyed lock 不能替代它。实例创建时固化 DefinitionVersion，定义的其他版本不影响运行中实例。

## 路由与汇聚

| 出边 | 构造 | 行为 |
| --- | --- | --- |
| 无条件 | `NewEdge(target)` | 总是激活，多条表示并行 |
| 条件 | `NewConditionalEdge(target, condition)` | 条件为真时激活 |
| 默认 | `NewDefaultEdge(target)` | 没有任何条件边命中时激活 |

每个节点最多一条默认边，默认边不得带条件。全条件出边且无无条件 / 默认出口的定义会被拒绝；运行期无出边命中返回 Conflict。

最多一条入边时 Kind 可省略，按 task 处理。多入边节点必须显式声明：NodeKindTask 为任一到达即可激活，NodeKindJoin 等待全部入边。

AdvanceNode 根据出边条件推进；AdvanceNodeTo 选择单个直接后继，不重复检查该边的条件，分支决策由调用方负责。RejectNode 只能回到 RejectTo 白名单中的严格支配祖先，并清理目标之后的运行状态。

## 内存示例

```go
package example

import (
	"context"
	"time"

	"gochen/process/workflow"
)

func RunApproval(ctx context.Context) error {
	engine := workflow.NewEngine(workflow.NewMemoryStore())
	definition := &workflow.Definition{
		ID:          "order_approval",
		StartNodeID: "review",
		Nodes: []workflow.Node{
			{
				ID:      "review",
				Timeout: 24 * time.Hour,
				Edges: []workflow.Edge{
					workflow.NewConditionalEdge("high", "data.amount >= 1000"),
					workflow.NewDefaultEdge("low"),
				},
			},
			{ID: "high"},
			{ID: "low"},
		},
	}
	if err := engine.SaveDefinition(ctx, definition); err != nil {
		return err
	}
	id := workflow.ID("wf-1")
	if err := engine.CreateInstanceWithData(ctx, id, definition.ID, map[string]any{"amount": 1500}); err != nil {
		return err
	}
	if err := engine.StartInstance(ctx, id); err != nil {
		return err
	}
	return engine.AdvanceNode(ctx, id, "review") // 激活 high 节点
}
```

## 钩子、数据与生命周期

`engine.Use` 的全局拦截器包住状态迁移，`OnNodeEnter` / `OnNodeExit` 在节点激活或离开时调用，随后保存状态。钩子失败中止迁移；已产生的外部副作用不会回滚，须幂等或在提交后执行。

NodeHookContext.Data 与 TransitionContext.Data 是快照，修改不写回实例。业务数据更新使用各 `*WithMutation` 入口的 StateMutation，并保持数据可稳定序列化。

SuspendInstance、ResumeInstance、TerminateInstance 控制实例生命周期。CreateInstance 只创建 pending，StartInstance 才激活起始节点。

## 超时与示例

CheckTimeouts 检查指定实例，ScanTimeouts(limit) 依赖 IQueryableStore 发现候选实例；两者只检测，由业务决定催办、推进或终止。扫描有界，按最早活动时刻取前 limit 条，不保证一次返回全部超时实例。

在 `gochen-runtime` 仓库执行 `GOWORK=off go run ./examples/process/workflow/basic` 查看完整示例。Core 验证入口为 `GOWORK=off go test -count=1 ./process/workflow`。
