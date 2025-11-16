package repository

import (
	"context"

	"gochen/domain/entity"
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
	Filters map[string]interface{}
}

// IQueryableRepository 可查询仓储接口（扩展接口）
// 提供更复杂的查询能力
type IQueryableRepository[T entity.IEntity[ID], ID comparable] interface {
	IRepository[T, ID]

	// Query 通用查询
	Query(ctx context.Context, opts QueryOptions) ([]T, error)

	// QueryOne 查询单条记录
	QueryOne(ctx context.Context, opts QueryOptions) (T, error)

	// QueryCount 查询计数
	QueryCount(ctx context.Context, opts QueryOptions) (int64, error)
}
