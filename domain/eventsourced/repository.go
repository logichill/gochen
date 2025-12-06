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

// Save 持久化聚合上的未提交事件。
//
// # expectedVersion 计算逻辑与隐式约定
//
// 本方法通过“当前聚合版本号”推导出事件存储中的 expectedVersion，从而实现乐观锁控制。
// 计算公式为：
//
//	expectedVersion = currentVersion - len(uncommittedEvents)
//
// 该公式依赖一个对所有事件溯源聚合都成立的隐式约定：
//
// ✅ 必须满足的约定：
//  1. 每次应用事件时，聚合的 ApplyEvent() 必须让版本号自增 1；
//  2. 聚合初始版本为 0（尚未应用任何事件）；
//  3. 任意时刻的版本号必须准确等于“已应用事件总数”。
//
// 示例：
//
//	初始状态：aggregate.version = 5（已持久化 5 条事件）
//	本次业务操作：生成 3 条新事件
//	ApplyEvent 调用：每条事件调用一次，内部 version++，最终版本 = 8
//	Save 计算：expectedVersion = 8 - 3 = 5
//	含义：期望事件存储当前版本为 5，即将追加第 6、7、8 条事件
//
// 如果事件存储中的版本不是 5（例如被其他事务改为 6），AppendEvents 将因并发冲突失败，
// 需要调用方重新加载聚合并重试操作。
//
// ⚠️ 常见错误：
//   - 在 ApplyEvent 中忘记递增版本：导致 expectedVersion 计算错误；
//   - 手工修改版本号而不走 ApplyEvent：破坏版本与事件数量的一致性；
//   - 不同事件类型对版本处理不一致：导致并发控制逻辑失效。
//
// 📝 建议：
//   - 将版本号递增逻辑统一实现到聚合基类中，具体聚合只负责状态变更；
//   - 在接口与文档中显式强调上述约定；
//   - 为聚合编写单元测试，验证 ApplyEvent 后版本号是否按预期递增。
func (r *EventSourcedRepository[T]) Save(ctx context.Context, aggregate T) error {
	events := aggregate.GetUncommittedEvents()
	if len(events) == 0 {
		return nil
	}

	// 防御性检查：在计算 expectedVersion 之前验证版本与事件数量的关系。
	currentVersion := aggregate.GetVersion()
	eventCount := uint64(len(events))

	// 断言：currentVersion 必须大于等于 eventCount。
	// 若不满足，通常说明聚合的 ApplyEvent 实现没有正确递增版本号。
	if currentVersion < eventCount {
		return fmt.Errorf(
			"version calculation error: currentVersion(%d) < eventCount(%d). This usually indicates that the ApplyEvent implementation of aggregate type %s does not correctly increment the version. Please check the implementation and ensure that each ApplyEvent call executes version++",
			currentVersion, eventCount, r.aggregateType,
		)
	}

	expectedVersion := currentVersion - eventCount

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
