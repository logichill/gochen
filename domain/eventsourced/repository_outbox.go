package eventsourced

import (
	"context"
	"fmt"

	"gochen/domain/entity"
	"gochen/eventing"
	"gochen/eventing/outbox"
	"gochen/logging"
)

// OutboxAwareRepository 为事件溯源仓储提供可选的 Outbox 原子发布支持。
//
// 设计目标：
//   - 保持现有 EventSourcedRepository 不变（向后兼容）
//   - 以装饰器方式启用 Outbox：Save 时不直接发布事件，改为在同一事务中
//     将事件写入事件存储与 Outbox 表，随后由 Outbox Publisher 异步发布
//   - Get/Exists/History 等读取路径复用基础仓储
//
// 注意：
// - outboxRepo 通常应基于 SQL 实现并与底层事件存储共享事务（例如 SimpleSQLOutboxRepository）
// - 若 outboxRepo 为 nil，则退化为基础仓储 Save 行为
type OutboxAwareRepository[T entity.IEventSourcedAggregate[int64]] struct {
	base   *EventSourcedRepository[T]
	outbox outbox.IOutboxRepository
	logger logging.ILogger
}

// NewOutboxAwareRepository 创建 Outbox 装饰器
func NewOutboxAwareRepository[T entity.IEventSourcedAggregate[int64]](base *EventSourcedRepository[T], outboxRepo outbox.IOutboxRepository) (*OutboxAwareRepository[T], error) {
	if base == nil {
		return nil, fmt.Errorf("base repository cannot be nil")
	}
	repo := &OutboxAwareRepository[T]{
		base:   base,
		outbox: outboxRepo,
		logger: base.logger,
	}
	if repo.logger == nil {
		repo.logger = logging.GetLogger().WithFields(
			logging.String("component", "eventsourced.repository.outbox"),
			logging.String("aggregate_type", base.aggregateType),
		)
	}
	return repo, nil
}

// Save 在启用 Outbox 时，使用 Outbox 仓储同事务写入事件与 Outbox 表；否则回退到基础仓储保存逻辑
func (r *OutboxAwareRepository[T]) Save(ctx context.Context, aggregate T) error {
	// 无 Outbox 时，直接回退到基础 Save
	if r.outbox == nil {
		return r.base.Save(ctx, aggregate)
	}

	// 收集未提交事件，并转换为值类型的 []eventing.Event 切片
	uncommitted := aggregate.GetUncommittedEvents()
	if len(uncommitted) == 0 {
		return nil
	}

	events := make([]eventing.Event, 0, len(uncommitted))
	for _, ie := range uncommitted {
		// 仅支持 *eventing.Event，保证可持久化
		if e, ok := ie.(*eventing.Event); ok && e != nil {
			// 复制为值类型，避免后续被修改
			events = append(events, *e)
			continue
		}
		return fmt.Errorf("outbox save requires *eventing.Event, got %T", ie)
	}

	// 使用 Outbox 同事务保存事件与 Outbox 记录
	if err := r.outbox.SaveWithEvents(ctx, aggregate.GetID(), events); err != nil {
		return err
	}

	// 标记事件为已提交；快照与发布策略复用基础仓储逻辑：
	// - 快照：调用基础仓储的快照判断与创建（非事务要求，允许失败告警）
	// - 发布：Outbox 模式下不再直接 Publish，由 Publisher 异步处理
	aggregate.MarkEventsAsCommitted()

	if r.base.snapshotManager != nil {
		currentVersion := uint64(aggregate.GetVersion())
		if should, err := r.base.snapshotManager.ShouldCreateSnapshot(ctx, snapshotAggregateAdapter[T]{aggregate: aggregate}); err == nil && should {
			if err := r.base.snapshotManager.CreateSnapshot(ctx, aggregate.GetID(), r.base.aggregateType, aggregate, currentVersion); err != nil {
				r.logger.Warn(ctx, "failed to create snapshot (outbox mode)",
					logging.Error(err),
					logging.Int64("aggregate_id", aggregate.GetID()),
					logging.Uint64("version", currentVersion))
			}
		}
	}
	return nil
}

// 读取相关方法全部委托给基础仓储
func (r *OutboxAwareRepository[T]) NewAggregate(id int64) T { return r.base.NewAggregate(id) }
func (r *OutboxAwareRepository[T]) GetByID(ctx context.Context, id int64) (T, error) {
	return r.base.GetByID(ctx, id)
}
func (r *OutboxAwareRepository[T]) Exists(ctx context.Context, id int64) (bool, error) {
	return r.base.Exists(ctx, id)
}
func (r *OutboxAwareRepository[T]) GetEventHistory(ctx context.Context, id int64) ([]eventing.IEvent, error) {
	return r.base.GetEventHistory(ctx, id)
}
func (r *OutboxAwareRepository[T]) GetEventHistoryAfter(ctx context.Context, id int64, afterVersion uint64) ([]eventing.IEvent, error) {
	return r.base.GetEventHistoryAfter(ctx, id, afterVersion)
}
func (r *OutboxAwareRepository[T]) GetAggregateVersion(ctx context.Context, id int64) (uint64, error) {
	return r.base.GetAggregateVersion(ctx, id)
}
