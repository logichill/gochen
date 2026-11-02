package crud

import (
	"context"

	"gochen/domain"
	"gochen/domain/meta"
	"gochen/domain/spec"
)

// DimensionChange 是迁移端口允许写入的维度目标值。
type DimensionChange struct {
	Dimension meta.Dimension
	Value     any
}

// TransferResult 保存互相独立的迁移前像、实际后像与实际变更。
type TransferResult[T any] struct {
	Before  T
	After   T
	Changes []DimensionChange
}

// ITransferRepository 提供版本约束下的独立维度迁移能力。
// 空变更、重复维度和无实际变化返回 InvalidInput。
// 调用方必须在包含业务与审计存储的同源事务中调用。
type ITransferRepository[T domain.IEntity[ID], ID comparable] interface {
	// CheckTransferEntry 在两阶段授权前验证入口处于事务外。
	// 仓储应同时检查上下文事务与自身绑定的事务会话。
	CheckTransferEntry(ctx context.Context) error
	Transfer(ctx context.Context, id ID, expectedVersion uint64, changes []DimensionChange, using, check spec.Spec) (TransferResult[T], error)
}
