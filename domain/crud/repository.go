// Package crud 提供面向简单 CRUD 场景的仓储抽象。
// 该包承载通用 CRUD 仓储接口与查询/批量/事务扩展，
// audited 与 eventsourced 可以在此基础上按需扩展能力。
package crud

import (
	"context"
	"gochen/domain"
)

// QueryOptions 查询选项
type QueryOptions struct {
	// Offset 偏移量
	Offset int

	// Limit 每页数量
	Limit int

	// OrderBy 排序字段
	OrderBy string

	// OrderDesc 是否降序
	OrderDesc bool

	// IncludeDeleted 是否包含已删除的记录（用于审计仓储）
	IncludeDeleted bool

	// Filters 过滤条件
	Filters map[string]any
}

// IRepository 简单 CRUD 仓储接口
// 适用于配置记录型数据（字典表、分类等）
type IRepository[T domain.IEntity[ID], ID comparable] interface {
	// Create 创建实体
	Create(ctx context.Context, e T) error

	// Update 更新实体
	Update(ctx context.Context, e T) error

	// Delete 物理删除实体
	Delete(ctx context.Context, id ID) error

	// Get 通过 ID 获取实体
	Get(ctx context.Context, id ID) (T, error)

	// List 分页查询（偏移量 + 限制）
	List(ctx context.Context, offset, limit int) ([]T, error)

	// Count 统计总数
	Count(ctx context.Context) (int64, error)

	// Exists 检查实体是否存在
	Exists(ctx context.Context, id ID) (bool, error)
}

// IQueryableRepository 可查询仓储接口（扩展接口）
// 提供更复杂的查询能力
type IQueryableRepository[T domain.IEntity[ID], ID comparable] interface {
	IRepository[T, ID]

	// Query 通用查询
	Query(ctx context.Context, opts QueryOptions) ([]T, error)

	// QueryOne 查询单条记录
	QueryOne(ctx context.Context, opts QueryOptions) (T, error)

	// QueryCount 查询计数
	QueryCount(ctx context.Context, opts QueryOptions) (int64, error)
}

// IBatchOperations 定义批量操作接口（可选扩展）
//
// 提供批量 CRUD 操作能力，用于提升大量数据操作的性能。
// 适用于需要批量导入、批量更新或批量删除的场景。
//
// 最佳实践：
//   - 批量操作应在事务中执行，保证原子性
//   - 考虑分批处理，避免单次操作数据量过大
//   - 提供进度回调或错误处理机制
type IBatchOperations[T domain.IEntity[ID], ID comparable] interface {
	// CreateAll 批量创建实体
	CreateAll(ctx context.Context, entities []T) error

	// UpdateBatch 批量更新实体
	UpdateBatch(ctx context.Context, entities []T) error

	// DeleteBatch 批量删除实体
	DeleteBatch(ctx context.Context, ids []ID) error
}

// ITransactional 定义支持事务的仓储接口（可选扩展）
//
// 提供事务管理能力，用于保证跨多个操作的数据一致性。
// 适用于需要原子性操作的复杂业务场景。
//
// 设计说明与注意事项：
//   - 接口通过 context 在调用方与仓储实现之间传递事务边界，容易因 ctx 误用导致“事务泄漏”或嵌套事务语义不清晰；
//   - 推荐在基础设施层直接使用 data/db 的事务抽象（ITransaction）或在应用服务层显式管理事务，
//     仅在确有需要时才在领域仓储接口上暴露 ITransactional，并配合清晰的使用约定与文档。
type ITransactional interface {
	// BeginTx 开始一个新事务
	BeginTx(ctx context.Context) (context.Context, error)

	// Commit 提交当前事务
	Commit(ctx context.Context) error

	// Rollback 回滚当前事务
	Rollback(ctx context.Context) error
}
