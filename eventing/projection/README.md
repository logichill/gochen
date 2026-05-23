# eventing/projection：投影与 ProjectionManager（CQRS 读模型）

投影（Projection）负责把事件流转换为读模型；`ProjectionManager` 负责投影的运行、错误处理、重放与检查点（checkpoint）。

## 1. 顶层概念

| 概念 | 对应类型 | 作用 |
|---|---|---|
| 投影 | `projection.IProjection[ID]` | 定义“如何处理事件/如何重建读模型” |
| 投影管理器 | `projection.ProjectionManager` | 注册投影、订阅事件、分发处理、管理状态 |
| 检查点 | `projection.ICheckpointStore` | 记录“处理到哪里”，用于恢复与追赶 |
| 配置 | `projection.ProjectionConfig` | 重试、死信、检查点保存策略、游标缺失策略 |

## 2. 运行模型（你需要掌握的语义）

- **订阅与分发**：`ProjectionManager` 会根据投影声明的 `SupportedEventTypes()` 在 `eventing/bus` 上订阅事件，并把事件分发给对应投影的 `Handle`。
- **最终一致**：投影通常异步更新读模型；业务需要接受读写延迟。
- **错误处理**：单事件失败支持重试；超过阈值进入死信回调（`DeadLetterFunc`）。
- **检查点**：可选启用；用于“进程重启后从上次位置继续”，避免重复全量重放。

## 3. 关键入口

- 构造管理器：
  - `projection.NewProjectionManager[ID](eventStore, eventBus, reg, upgraders)`
  - `projection.NewProjectionManagerWithConfig[ID](eventStore, eventBus, reg, upgraders, cfg)`
- 配置预设（checkpoint 保存频率）：
  - `projection.ProjectionConfigPresets.LowLatency()`
  - `projection.ProjectionConfigPresets.Balanced()`
  - `projection.ProjectionConfigPresets.HighThroughput()`
- 启用 checkpoint：
  - `pm, err = pm.WithCheckpointStore(store)`（内存/SQL 见 `checkpoint_*`）
  - 启用后，投影必须实现 `ICheckpointingProjection`；manager 不再代投影做 best-effort checkpoint 保存

## 4. 最小示例（骨架）

```go
type UserViewProjection struct{}

func (p *UserViewProjection) Name() string { return "user_view" }
func (p *UserViewProjection) SupportedEventTypes() []string { return []string{"UserCreated", "UserUpdated"} }
func (p *UserViewProjection) Status() projection.ProjectionStatus { return projection.ProjectionStatus{Name: p.Name()} }

func (p *UserViewProjection) Handle(ctx context.Context, evt eventing.IEvent) error {
	// 这里写“增量更新读模型”的逻辑
	return nil
}

func (p *UserViewProjection) Rebuild(ctx context.Context, events []eventing.Event[int64]) error {
	// 这里写“全量重建读模型”的逻辑（通常先清表/重置，再回放）
	return nil
}

pm, err := projection.NewProjectionManagerWithConfig[int64](eventStore, eventBus, reg, upgraders, projection.ProjectionConfigPresets.Balanced())
if err != nil {
	return err
}
pm, err = pm.WithCheckpointStore(checkpointStore)
if err != nil {
	return err
}

txRunner, err := projection.NewSQLCheckpointTxRunner(ormEngine)
if err != nil {
	return err
}
userView, err := projection.NewCheckpointingProjector[int64](&UserViewProjection{}, txRunner)
if err != nil {
	return err
}

_ = pm.RegisterProjection(userView)
_ = pm.StartProjection("user_view")
defer pm.StopProjection("user_view")
```

> 完整可运行示例建议直接看：`examples/infra/projection/*`。

## 5. checkpoint 游标缺失（fail-fast）

当启用 checkpoint 后，恢复逻辑会使用 `checkpoint.LastEventID` 去事件存储拉取“后续事件”。

如果事件存储返回 `NotFound`（常见于事件被清理/归档），`ResumeFromCheckpoint` 会直接返回错误（fail-fast）。

如需重建，请由调用方显式触发（例如：删除 checkpoint 后离线重建，或在业务侧调用 `RebuildProjection`）。

## 6. 进一步阅读

- 设计与边界：`docs/framework-design.md`
- 示例：`examples/infra/projection/basic`、`examples/infra/projection/idempotent`、`examples/infra/projection/sql_checkpoint`

## 并发与线程安全（契约）

- `ProjectionManager` 以 per-projection runtime 管理投影注册表、状态、handler、checkpoint tracker 与内存 cursor；`Register/Unregister/Start/Stop/GetStatus*` 可并发调用，但推荐在“装配期”完成注册与配置。
- 同一 projection 的在线处理、checkpoint 追赶、`ResumeFromCheckpoint` 与 `RebuildProjection` 串行执行；不同 projection 之间可并发。
- checkpoint 模式下，在线 handler 仅在 projection 为 running 时唤醒 runtime 从 EventStore 按 checkpoint cursor 追赶，并优先使用 runtime 内存 cursor 跨越尚未持久化的批量 checkpoint 窗口；显式 `ResumeFromCheckpoint` 在运行期按 runtime/durable 较新游标继续，冷启动才只由 durable checkpoint 初始化；启用 checkpoint store 时必须配置 `IEventStreamStore`。未启用 checkpoint 的 live-only 模式只保证同一 projection 不重入，不承诺崩溃恢复。
- `RebuildProjection` 会把投影状态标记为 `rebuilding`，在线处理会等待同一 runtime 串行边界；重建完成后状态回到 `stopped`，需要显式 `StartProjection` 才会继续处理。

相关竞态回归测试：`eventing/projection/manager_race_test.go`。
