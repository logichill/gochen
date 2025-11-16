package repository

import (
	"context"

	"gochen/domain/entity"
	"gochen/eventing"
)

// IEventSourcedRepository 事件溯源仓储接口
// 适用于完全审计型数据（金融交易、积分系统等）
type IEventSourcedRepository[T entity.IEventSourcedAggregate[ID], ID comparable] interface {
	// Save 保存聚合（保存事件，不保存状态）
	// 通过追加事件到 EventStore 实现持久化
	Save(ctx context.Context, aggregate T) error

	// GetByID 通过 ID 获取聚合
	// 通过重放事件重建聚合状态
	GetByID(ctx context.Context, id ID) (T, error)

	// Exists 检查聚合是否存在
	Exists(ctx context.Context, id ID) (bool, error)

	// GetEventHistory 获取聚合的事件历史
	GetEventHistory(ctx context.Context, id ID) ([]eventing.IEvent, error)

	// GetEventHistoryAfter 获取指定版本之后的事件历史
	GetEventHistoryAfter(ctx context.Context, id ID, afterVersion uint64) ([]eventing.IEvent, error)

	// GetAggregateVersion 获取聚合的当前版本号
	GetAggregateVersion(ctx context.Context, id ID) (uint64, error)
}
