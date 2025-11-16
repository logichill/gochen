package repository

import (
	"context"

	"gochen/domain/entity"
)

// IRepository 简单 CRUD 仓储接口
// 适用于配置记录型数据（字典表、分类等）
type IRepository[T entity.IEntity[ID], ID comparable] interface {
	// Create 创建实体
	Create(ctx context.Context, e T) error

	// GetByID 通过 ID 获取实体
	GetByID(ctx context.Context, id ID) (T, error)

	// Update 更新实体
	Update(ctx context.Context, e T) error

	// Delete 物理删除实体
	Delete(ctx context.Context, id ID) error

	// List 分页查询（偏移量 + 限制）
	List(ctx context.Context, offset, limit int) ([]T, error)

	// Count 统计总数
	Count(ctx context.Context) (int64, error)

	// Exists 检查实体是否存在
	Exists(ctx context.Context, id ID) (bool, error)
}
