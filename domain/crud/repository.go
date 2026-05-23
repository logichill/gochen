// Package crud 提供面向简单 CRUD 场景的仓储抽象。
// 该包承载通用 CRUD 仓储接口与查询/批量扩展，
// audited 与 eventsourced 可以在此基础上按需扩展能力。
package crud

import (
	"context"
	"gochen/domain"
)

// IRepository 简单 CRUD 仓储接口。
// 适用于配置记录型数据（字典表、分类等）
//
// # 错误契约。
//
// 所有方法统一使用 gochen/errors 错误码，调用方可通过 errors.Is(err, code) 判断：
//
//   - Get: 未找到返回 errors.NotFound（返回零值 + NotFound）
//   - Create: 已存在/唯一约束冲突返回 errors.Conflict；其他为基础设施错误。
//   - Update: 未找到返回 errors.NotFound；乐观锁/并发写冲突返回 errors.Concurrency
//   - Delete: 默认实现可幂等（未找到不报错）；若实现选择严格语义，可在未找到时返回 errors.NotFound
//   - List/Count/Exists: 不属于基础写模型仓储，见可选扩展 IQueryRepository
type IRepository[T domain.IEntity[ID], ID comparable] interface {
	// Create 创建实体
	Create(ctx context.Context, e T) error

	// Update 更新实体
	Update(ctx context.Context, e T) error

	// Delete 删除实体。
	//
	// 说明：
	// - Delete 表示“业务删除”语义：具体是软删还是物理删除，由仓储实现决定；
	// - 若需要明确的“物理删除（不可恢复）”语义，请使用可选扩展 `IPurgeRepository` 的 `Purge`。
	Delete(ctx context.Context, id ID) error

	// Get 通过 ID 获取实体
	Get(ctx context.Context, id ID) (T, error)
}

// IQueryRepository 定义 CRUD 场景下的可选读扩展能力。
//
// 说明：
// - 将列表、计数、存在性检查从基础 IRepository 中拆出，避免所有仓储默认承载 UI/列表查询语义；
// - Application / API 可在需要时通过类型断言探测该能力并做 fail-fast 处理。
type IQueryRepository[T domain.IEntity[ID], ID comparable] interface {
	// List 分页查询（偏移量 + 限制）
	List(ctx context.Context, offset, limit int) ([]T, error)

	// Count 统计总数
	Count(ctx context.Context) (int64, error)

	// Exists 检查实体是否存在
	Exists(ctx context.Context, id ID) (bool, error)
}

// IPurgeRepository 定义“物理删除（不可恢复）”的可选扩展能力。
//
// 说明：
// - 当实体具备软删能力时，Delete 往往实现为软删；此时 Purge 用于明确表达“永久删除”；
// - Purge 不进入基础 IRepository，以避免将“危险操作”默认为所有仓储必须实现。
type IPurgeRepository[T domain.IEntity[ID], ID comparable] interface {
	// Purge 永久删除实体（物理删除，不可恢复）。
	Purge(ctx context.Context, id ID) error
}

// IBatchOperations 定义批量操作接口（可选扩展）
//
// 提供批量 CRUD 操作能力，用于提升大量数据操作的性能。
// 适用于需要批量导入、批量更新或批量删除的场景。
//
// 最佳实践：
//   - 批量操作应在事务中执行，保证原子性。
//   - 考虑分批处理，避免单次操作数据量过大。
//   - 提供进度回调或错误处理机制。
type IBatchOperations[T domain.IEntity[ID], ID comparable] interface {
	// CreateAll 批量创建实体
	CreateAll(ctx context.Context, entities []T) error

	// UpdateAll 批量更新实体
	UpdateAll(ctx context.Context, entities []T) error

	// DeleteAll 批量删除实体
	DeleteAll(ctx context.Context, ids []ID) error
}
