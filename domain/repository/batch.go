package repository

import (
	"context"

	"gochen/domain/entity"
)

// IBatchOperations 定义批量操作接口（可选扩展）
//
// 提供批量 CRUD 操作能力，用于提升大量数据操作的性能。
// 适用于需要批量导入、批量更新或批量删除的场景。
//
// 最佳实践：
//   - 批量操作应在事务中执行，保证原子性
//   - 考虑分批处理，避免单次操作数据量过大
//   - 提供进度回调或错误处理机制
type IBatchOperations[T entity.IEntity[ID], ID comparable] interface {
	// CreateAll 批量创建实体
	//
	// 参数：
	//   - ctx: 上下文
	//   - entities: 待创建的实体列表
	//
	// 返回：
	//   - error: 创建失败时返回错误，应支持部分成功的错误信息
	CreateAll(ctx context.Context, entities []T) error

	// UpdateBatch 批量更新实体
	//
	// 参数：
	//   - ctx: 上下文
	//   - entities: 待更新的实体列表
	//
	// 返回：
	//   - error: 更新失败时返回错误
	UpdateBatch(ctx context.Context, entities []T) error

	// DeleteBatch 批量删除实体
	//
	// 参数：
	//   - ctx: 上下文
	//   - ids: 待删除的实体ID列表
	//
	// 返回：
	//   - error: 删除失败时返回错误
	DeleteBatch(ctx context.Context, ids []ID) error
}
