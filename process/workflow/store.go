package workflow

import (
	"context"
	"time"
)

// IStore 定义工作流定义（支持多版本）与实例的最小存储接口。
//
// 该接口只适用于测试或保证单进程访问的存储。生产环境和任何多实例部署必须实现
// IOptimisticStore；Engine 的 keyed lock 不能替代跨进程并发控制。
type IStore interface {
	// GetDefinition 按定义 ID 与版本号查询工作流定义。
	// 当 version == 0 时返回该 ID 的最新版本；不存在时返回 nil。
	GetDefinition(ctx context.Context, id string, version uint32) (*Definition, error)

	// SaveDefinition 保存工作流定义。
	SaveDefinition(ctx context.Context, def *Definition) error

	// DeleteDefinition 删除工作流定义。
	// 当 version == 0 时删除该 ID 的所有版本；指定版本时只删除特定版本。
	DeleteDefinition(ctx context.Context, id string, version uint32) error

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

// InstanceQuery 描述实例列表查询条件；各字段之间是 AND 关系，零值表示不过滤。
type InstanceQuery struct {
	// DefinitionID 只返回绑定该流程定义的实例。
	DefinitionID string

	// Statuses 只返回处于其中任一状态的实例。
	Statuses []InstanceStatus

	// ActiveSince 只返回存在"激活时刻早于或等于该时间"的活动节点的实例。
	ActiveSince time.Time

	// Limit 限制返回条数；必须为正数。
	Limit int
}

// IQueryableStore 在 IStore 基础上提供实例枚举能力。
//
// 后台巡检类能力（超时发现、卡单排查）依赖该接口：没有它，调用方只能在已知实例 ID
// 的前提下做点检，无法回答"哪些实例超时了"。实现应按最早活动时刻升序返回，
// 使最可能超时的实例优先出现在有界扫描结果中。
type IQueryableStore interface {
	IStore

	// ListInstances 按查询条件返回实例状态列表。
	ListInstances(ctx context.Context, query InstanceQuery) ([]*State, error)
}
