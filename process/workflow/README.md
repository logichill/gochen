# Workflow

`process/workflow` 提供轻量流程编排与状态机内核，用于保存多版本流程定义、创建与启动实例、基于条件/网关推进节点、显式选择分支、严格支配驳回回退、实例挂起/恢复/终止生命周期控制、节点超时检测与巡检、全局拦截器与节点级钩子流水线。

## 核心概念

| 概念 | 责任 |
|---|---|
| `Definition` | 描述流程图结构、不可变递增版本号（`Version`）、起始节点与节点出边集合 |
| `Node` | 表示一个可推进节点；`Edges` 是后继出边与条件，`RejectTo` 是允许驳回的目标，`Timeout` 是节点停留超时时限 |
| `Edge` | 描述一条后继出边：`Target` 目标节点、可选 `Condition` 条件表达式、`Default` 标记 else 分支 |
| `State` | 保存实例状态、绑定的定义版本号（`DefinitionVersion`）、活动节点及激活时刻、已完成节点、等待中的 join 与轨迹历史 |
| `Engine` | 负责定义校验、多版本隔离、实例生命周期（创建/启动/推进/驳回/挂起/恢复/终止）与拦截器、钩子流水线 |
| `IConditionEvaluator` | 条件表达式求值 SPI 接口，默认提供零依赖标准求值器 |
| `IConditionValidator` | 求值器可选扩展：在保存定义时静态校验条件语法，把表达式写错前移到定义期 |
| `Interceptor` | 全局状态迁移拦截器（中间件洋葱圈模型），用于统一审计、追踪、权限校验与指标统计 |
| `NodeHook` | 节点级钩子（`OnNodeEnter` / `OnNodeExit`），用于节点开始/结束语义的分派、校验与资源收尾 |
| `IStore` / `IOptimisticStore` | 多版本定义与实例状态存储接口，生产环境必须实现 `IOptimisticStore` 版本化存储契约 |
| `IQueryableStore` | 存储可选扩展：枚举实例，支撑 `ScanTimeouts` 等后台巡检"发现"能力 |

## 三类出边语义

| 出边 | 构造 | 运行期语义 |
|---|---|---|
| 无条件边 | `NewEdge(t)` | 总是激活；同一节点多条无条件边表示**并行分支** |
| 条件边 | `NewConditionalEdge(t, cond)` | 条件为真时激活 |
| 默认边 | `NewDefaultEdge(t)` | 仅当该节点没有任何条件边命中时激活，即 **else 分支**；每节点最多一条，且不得带条件 |

**定义期护栏**：若某节点的出边全部是条件边（既无无条件边也无默认边），`SaveDefinition` 会直接拒绝并要求补一条默认边。原因是引擎无法证明条件集合互斥且穷尽，一旦业务数据落在所有条件之外，实例就会推进到"无处可去"——这会被误判为流程正常完成。宁可在定义期报错，也不要在运行期静默走空。

## 推进与生命周期语义

- **多版本隔离**：实例创建时固化绑定 `DefinitionVersion`，流程定义升级不影响既有运行中实例。
- **条件路由**：出边配置 `Condition` 表达式后，引擎自动根据实例业务数据求值并路由；条件全不命中时走默认边，两者都没有则返回 `Conflict` 错误而**不会**静默完成。也可通过 `AdvanceNodeTo` 显式指定单个后继（显式选择由调用方承担决策责任，不再校验该出边的条件）。
- **分支与汇聚**：多入边节点必须显式声明 `Kind`：`NodeKindTask` 表示任一入边到达即可继续，`NodeKindJoin` 表示必须等待全部入边。
- **严格支配驳回**：`RejectNode` 只能驳回到 `RejectTo` 白名单中的严格支配祖先，并自动清理目标节点之后的运行状态与激活时间。
- **生命周期控制**：提供 `SuspendInstance`（挂起）、`ResumeInstance`（恢复）、`TerminateInstance`（显式终止）。
- **超时**：`CheckTimeouts(instanceID)` 对已知实例做点检；`ScanTimeouts(limit)` 面向后台巡检，负责"发现"哪些实例超时（要求 store 实现 `IQueryableStore`，否则返回 `Unsupported`）。两者只做检测与上报，处置动作由调用方决定。
- **拦截器与钩子**：`engine.Use(...)` 注册全局拦截器，包住整次状态迁移；`engine.OnNodeEnter/OnNodeExit` 注册节点级钩子，在节点激活/离开时触发。嵌套关系为 `拦截器 → 节点钩子 → 状态持久化`；钩子返回错误会中止整次迁移，实例保持迁移前状态。
- **钩子是 at-least-once**：钩子成功返回后，同一次迁移仍可能因后续钩子失败或持久化版本冲突整体回滚，已执行的副作用不会被撤销。带外部副作用的钩子必须自身幂等，或把副作用改为写入实例数据、由调用方在提交后另行触发。
- **钩子数据只读**：`NodeHookContext.Data` 与 `TransitionContext.Data` 都是实例数据的**快照**，改写不会写回实例；需要变更实例数据请使用 `*WithMutation` 系列入口的 `StateMutation`。

> **内核边界**：workflow 只管理"流程推进到哪一步"，**不执行**节点业务逻辑——引擎没有动作执行 SPI。节点该做什么由调用方在钩子里实现，或由调用方在推进前后自行编排（跨步骤补偿型编排见 `process/saga`）。

## 最小示例

```go
store := workflow.NewMemoryStore() // 生产环境替换为 runtime/process/workflow/sqlstore
engine := workflow.NewEngine(store)

// 1. 注册全局拦截器与节点级钩子（可选）
engine.Use(func(ctx context.Context, tctx *workflow.TransitionContext, next func(context.Context) error) error {
	log.Printf("[workflow] action=%s instance=%s node=%s", tctx.Action, tctx.InstanceID, tctx.NodeID)
	return next(ctx)
})
engine.OnNodeEnter("approved_high", func(ctx context.Context, hctx *workflow.NodeHookContext) error {
	// 节点开始：分派处理人、下发通知；返回错误可否决本次推进
	return notifyDirector(ctx, hctx.InstanceID, hctx.Data["amount"])
})

// 2. 保存流程定义（条件路由 + 默认分支 + 节点超时）
definition := &workflow.Definition{
	ID:          "order_approval",
	StartNodeID: "review",
	Nodes: []workflow.Node{
		{
			ID:      "review",
			Timeout: 24 * time.Hour,
			Edges: []workflow.Edge{
				workflow.NewConditionalEdge("approved_high", "data.amount >= 1000"),
				workflow.NewDefaultEdge("approved_low"), // else 分支，缺失则定义校验失败
			},
		},
		{ID: "approved_high"},
		{ID: "approved_low"},
	},
}
if err := engine.SaveDefinition(ctx, definition); err != nil {
	return err
}

// 3. 创建并启动实例（带业务数据）
instanceData := map[string]any{"amount": 1500}
if err := engine.CreateInstanceWithData(ctx, workflow.ID("wf-1"), definition.ID, instanceData); err != nil {
	return err
}
if err := engine.StartInstance(ctx, workflow.ID("wf-1")); err != nil {
	return err
}

// 4. 推进节点（自动评估条件出边走向 approved_high）
return engine.AdvanceNode(ctx, workflow.ID("wf-1"), "review")
```

## 后台超时巡检

```go
// 要求 store 实现 IQueryableStore（sqlstore.SQLStore 已实现）
timedOut, err := engine.ScanTimeouts(ctx, 100)
if err != nil {
	return err
}
for _, item := range timedOut {
	// 处置策略由业务决定：催办、自动通过、驳回或终止
	log.Printf("instance %s timed out at %v", item.InstanceID, item.NodeIDs)
}
```

单次调用是**有界扫描**（按最早活动时刻升序取前 `limit` 条），不保证返回全部超时实例，应按固定周期重复执行。

## 可运行示例

```bash
go run ./examples/process/workflow/basic
```

