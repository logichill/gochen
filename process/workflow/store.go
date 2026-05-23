package workflow

import "context"

// IStore 定义工作流定义与实例的最小存储接口。
type IStore interface {
	// GetDefinition 按定义 ID 查询工作流定义；不存在时返回 nil。
	GetDefinition(ctx context.Context, id string) (*Definition, error)

	// SaveDefinition 保存或覆盖工作流定义。
	SaveDefinition(ctx context.Context, def *Definition) error

	// DeleteDefinition 删除工作流定义。
	DeleteDefinition(ctx context.Context, id string) error

	// Get 按实例 ID 查询工作流状态；不存在时返回 nil。
	Get(ctx context.Context, id ID) (*State, error)

	// Save 保存或覆盖工作流实例状态。
	Save(ctx context.Context, st *State) error

	// Delete 删除工作流实例状态。
	Delete(ctx context.Context, id ID) error
}

// IOptimisticStore 在 IStore 基础上提供版本化保存能力。
type IOptimisticStore interface {
	IStore

	// SaveIfVersion 在当前版本等于 expectedVersion 时保存状态，并递增版本。
	SaveIfVersion(ctx context.Context, st *State, expectedVersion uint64) error
}
