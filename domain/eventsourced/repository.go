package eventsourced

import (
	"context"
	"fmt"

	"gochen/domain"
)

// IEventSourcedRepository 事件溯源仓储接口
// 适用于完全审计型数据（金融交易、积分系统等）
type IEventSourcedRepository[T IEventSourcedAggregate[ID], ID comparable] interface {
	// Save 保存聚合（保存事件，不保存状态）。
	Save(ctx context.Context, aggregate T) error

	// GetByID 通过 ID 获取聚合。
	// 具体实现通常通过重放事件重建聚合状态。
	GetByID(ctx context.Context, id ID) (T, error)

	// Exists 检查聚合是否存在。
	Exists(ctx context.Context, id ID) (bool, error)

	// GetAggregateVersion 获取聚合的当前版本号。
	// 若聚合不存在，应返回 (0, nil)。
	GetAggregateVersion(ctx context.Context, id ID) (uint64, error)
}

// IEventStore 领域层的事件存储抽象。
//
// 注意：该接口以领域事件（IDomainEvent）为中心，不关心具体存储实现与传输信封，
// 由上层通过适配器对接 eventing/store.IEventStore、Outbox、Snapshot 等基础设施。
type IEventStore interface {
	// AppendEvents 追加领域事件到聚合的事件流中。
	AppendEvents(ctx context.Context, aggregateID int64, events []domain.IDomainEvent, expectedVersion uint64) error

	// RestoreAggregate 根据底层事件流（及可选快照）恢复聚合状态。
	// 若聚合不存在，应返回 (0, nil) 并保持 aggregate 为初始状态。
	//
	// 返回值为当前聚合版本号（即最后一个事件的版本），用于上层判断是否存在或做乐观锁控制。
	RestoreAggregate(ctx context.Context, aggregate IEventSourcedAggregate[int64]) (uint64, error)

	// Exists 检查聚合是否存在。
	Exists(ctx context.Context, aggregateID int64) (bool, error)

	// GetAggregateVersion 获取聚合当前版本。
	// 若聚合不存在，应返回 (0, nil)。
	GetAggregateVersion(ctx context.Context, aggregateID int64) (uint64, error)
}

// EventSourcedRepository 领域层默认事件溯源仓储实现。
//
// 该实现仅依赖领域抽象：
//   - IEventSourcedAggregate[int64]：聚合根；
//   - IEventStore：领域事件存储接口。
//
// 具体的事件存储/快照/Outbox/EventBus 等能力由上层通过 IEventStore 适配器提供。
type EventSourcedRepository[T IEventSourcedAggregate[int64]] struct {
	aggregateType string
	factory       func(id int64) T
	store         IEventStore
}

// NewEventSourcedRepository 创建领域层默认事件溯源仓储。
func NewEventSourcedRepository[T IEventSourcedAggregate[int64]](
	aggregateType string,
	factory func(id int64) T,
	store IEventStore,
) (*EventSourcedRepository[T], error) {
	if aggregateType == "" {
		return nil, fmt.Errorf("aggregate type cannot be empty")
	}
	if factory == nil {
		return nil, fmt.Errorf("aggregate factory cannot be nil")
	}
	if store == nil {
		return nil, fmt.Errorf("event store cannot be nil")
	}
	return &EventSourcedRepository[T]{
		aggregateType: aggregateType,
		factory:       factory,
		store:         store,
	}, nil
}

// Save 保存聚合的未提交事件。
func (r *EventSourcedRepository[T]) Save(ctx context.Context, aggregate T) error {
	events := aggregate.GetUncommittedEvents()
	if len(events) == 0 {
		return nil
	}

	currentVersion := uint64(aggregate.GetVersion())
	var expectedVersion uint64
	if currentVersion >= uint64(len(events)) {
		expectedVersion = currentVersion - uint64(len(events))
	}

	if err := r.store.AppendEvents(ctx, aggregate.GetID(), events, expectedVersion); err != nil {
		return err
	}

	aggregate.MarkEventsAsCommitted()
	return nil
}

// GetByID 根据 ID 加载聚合（通过 RestoreAggregate 恢复）。
func (r *EventSourcedRepository[T]) GetByID(ctx context.Context, id int64) (T, error) {
	aggregate := r.factory(id)
	if _, err := r.store.RestoreAggregate(ctx, aggregate); err != nil {
		return aggregate, err
	}
	return aggregate, nil
}

// Exists 检查聚合是否存在。
func (r *EventSourcedRepository[T]) Exists(ctx context.Context, id int64) (bool, error) {
	return r.store.Exists(ctx, id)
}

// GetAggregateVersion 获取聚合当前版本。
func (r *EventSourcedRepository[T]) GetAggregateVersion(ctx context.Context, id int64) (uint64, error) {
	version, err := r.store.GetAggregateVersion(ctx, id)
	if err != nil {
		return 0, err
	}
	// 语义约定：不存在返回 0。
	return version, nil
}

// Ensure interface compliance.
var _ IEventSourcedRepository[IEventSourcedAggregate[int64], int64] = (*EventSourcedRepository[IEventSourcedAggregate[int64]])(nil)
