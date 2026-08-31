package saga

import "context"

// ISagaStateStore 抽象Saga状态存储能力接口。
type ISagaStateStore interface {
	// Load 加载 Saga 状态。
	//
	// 参数：
	//   - ctx: 上下文。
	//   - sagaID: Saga ID。
	//
	// 返回：
	//   - *SagaState: 状态数据。
	//   - error: ErrSagaNotFound 表示不存在，其他错误表示存储失败。
	Load(ctx context.Context, sagaID string) (*SagaState, error)

	// Save 保存 Saga 状态。
	//
	// 参数：
	//   - ctx: 上下文。
	//   - state: 状态数据。
	//
	// 返回：
	//   - error: 保存失败错误。
	//
	// 语义约定：
	//   - Save 仅用于创建 Saga 初始状态。
	//   - 若相同 SagaID 已存在，应该返回 Conflict，而不是覆盖既有进度。
	Save(ctx context.Context, state *SagaState) error

	// Update 更新 Saga 状态。
	//
	// 参数：
	//   - ctx: 上下文。
	//   - state: 状态数据。
	//
	// 返回：
	//   - error: 更新失败错误。
	//
	// 语义约定：
	//   - Update 仅用于已存在的 Saga 状态，通常在执行步骤或补偿步骤后调用。
	//   - 与 Save 不同，Update 失败通常应视为严重错误（需要中止 Saga 或告警）。
	Update(ctx context.Context, state *SagaState) error

	// Delete 删除 Saga 状态。
	//
	// 参数：
	//   - ctx: 上下文。
	//   - sagaID: Saga ID。
	//
	// 返回：
	//   - error: 删除失败错误。
	Delete(ctx context.Context, sagaID string) error

	// List 列出所有 Saga 状态（可选，用于监控）。
	//
	// 参数：
	//   - ctx: 上下文。
	//   - status: 状态过滤（可选）。
	//
	// 返回：
	//   - []*SagaState: 状态列表。
	//   - error: 查询失败错误。
	List(ctx context.Context, status SagaStatus) ([]*SagaState, error)
}
