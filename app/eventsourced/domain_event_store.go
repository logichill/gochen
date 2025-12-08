package eventsourced

import (
	"context"
	"errors"
	"fmt"

	"gochen/domain"
	deventsourced "gochen/domain/eventsourced"
	appErrors "gochen/errors"
	"gochen/eventing"
	"gochen/eventing/bus"
	"gochen/eventing/outbox"
	"gochen/eventing/store"
	"gochen/eventing/store/snapshot"
	"gochen/logging"
)

// DomainEventStoreOptions 配置领域层 IEventStore 的基础设施适配。
// 用于构造基于 eventing/store + Outbox + EventBus 的事件存储实现。
//
// 设计目标：
//   - 对领域层暴露为 deventsourced.IEventStore[ID]，ID 可以是任意可比较类型（如 int64/string/自定义类型）；
//   - 在内部直接使用与领域层一致的 ID 类型贯通 eventing/store.IEventStore[ID]、SnapshotManager[ID] 与 Outbox 仓储。
//
// 注意：
//   - 本仓库内置的事件存储与 Outbox/快照实现当前仍主要以 ID=int64 形式实例化；
//   - 若业务需要使用非 int64 的聚合 ID（如 string/UUID），需要提供对应 ID 形态的 IEventStore[ID]、ISnapshotStore[ID] 与 IOutboxRepository[ID] 实现。
type DomainEventStoreOptions[T deventsourced.IEventSourcedAggregate[ID], ID comparable] struct {
	AggregateType   string
	Factory         func(id ID) T
	EventStore      store.IEventStore[ID]
	SnapshotManager *snapshot.Manager[ID]
	EventBus        bus.IEventBus
	OutboxRepo      outbox.IOutboxRepository[ID]
	PublishEvents   bool
	Logger          logging.ILogger
}

// Deprecated: 请使用 DomainEventStoreOptions。
type EventStoreAdapterOptions[T deventsourced.IEventSourcedAggregate[ID], ID comparable] = DomainEventStoreOptions[T, ID]

// DomainEventStore 将 eventing/store.IEventStore[ID] + Snapshot/Outbox/EventBus
// 适配为领域层的 deventsourced.IEventStore[ID]。
type DomainEventStore[T deventsourced.IEventSourcedAggregate[ID], ID comparable] struct {
	aggregateType   string
	factory         func(id ID) T
	eventStore      store.IEventStore[ID]
	snapshotManager *snapshot.Manager[ID]
	eventBus        bus.IEventBus
	outboxRepo      outbox.IOutboxRepository[ID]
	publishEvents   bool
	logger          logging.ILogger
}

// NewDomainEventStore 创建领域层 IEventStore[ID] 的基础设施实现。
func NewDomainEventStore[T deventsourced.IEventSourcedAggregate[ID], ID comparable](
	opts DomainEventStoreOptions[T, ID],
) (deventsourced.IEventStore[ID], error) {
	if opts.AggregateType == "" {
		return nil, fmt.Errorf("aggregate type cannot be empty")
	}
	if opts.Factory == nil {
		return nil, fmt.Errorf("aggregate factory cannot be nil")
	}
	if opts.EventStore == nil {
		return nil, fmt.Errorf("event store cannot be nil")
	}

	// 强约束：当需要通过 EventBus 发布事件时，必须配置 OutboxRepo。
	// 只要 PublishEvents=true 且提供了 EventBus，就视为“需要对外发布事件”，
	// 此时缺少 OutboxRepo 将被视为配置错误并在装配阶段直接失败。
	if opts.PublishEvents && opts.EventBus != nil && opts.OutboxRepo == nil {
		return nil, fmt.Errorf("event publishing requires outbox repository: PublishEvents=true and EventBus is configured but OutboxRepo is nil")
	}

	adapter := &DomainEventStore[T, ID]{
		aggregateType:   opts.AggregateType,
		factory:         opts.Factory,
		eventStore:      opts.EventStore,
		snapshotManager: opts.SnapshotManager,
		eventBus:        opts.EventBus,
		outboxRepo:      opts.OutboxRepo,
		publishEvents:   opts.PublishEvents,
		logger:          opts.Logger,
	}
	if adapter.logger == nil {
		adapter.logger = logging.ComponentLogger("app.eventsourced.domain_event_store").
			WithField("aggregate_type", opts.AggregateType)
	}

	return adapter, nil
}

// NewEventStoreAdapter 为兼容旧用法保留的构造函数。
//
// Deprecated: 请使用 NewDomainEventStore。
func NewEventStoreAdapter[T deventsourced.IEventSourcedAggregate[ID], ID comparable](
	opts DomainEventStoreOptions[T, ID],
) (deventsourced.IEventStore[ID], error) {
	return NewDomainEventStore(opts)
}

// AppendEvents 将领域事件追加到事件存储中。
//
// 支持两种模式：
//   - 配置 OutboxRepo 时：在同一数据库事务中同时写入事件与 Outbox 记录，保证原子性；
//   - 未配置 OutboxRepo 时：先直接调用 EventStore.AppendEvents 持久化事件，再尝试通过 EventBus 发布事件。
//
// ⚠️ 注意：未启用 Outbox 的模式存在原子性风险
//
// 当 PublishEvents=true 且 OutboxRepo=nil 时，事件持久化与发布是两个独立步骤：
//  1. EventStore.AppendEvents(ctx, ...)  // 事件写入成功
//  2. EventBus.Publish(ctx, ...)         // 发布可能失败
//
// 如果第 1 步成功而第 2 步失败：
//   - 事件已经持久化，但消息总线未收到通知；
//   - 下游系统不会感知到这批事件；
//   - 需要额外补偿机制或通过重放/扫描来补发事件。
//
// 在生产环境强烈建议使用 Outbox 模式，以保证“最终一致”的语义。
func (a *DomainEventStore[T, ID]) AppendEvents(ctx context.Context, aggregateID ID, events []domain.IDomainEvent, expectedVersion uint64) error {
	if len(events) == 0 {
		return nil
	}

	currentVersion := expectedVersion
	storableEvents := make([]eventing.Event[ID], 0, len(events))

	// 仅在需要直接通过 EventBus 发布且未启用 Outbox 时才构建发布用切片，
	// Outbox 模式下不再为 Publish 分配额外 slice，避免不必要的内存开销。
	needDirectPublish := a.publishEvents && a.eventBus != nil && a.outboxRepo == nil
	var publishedEvents []eventing.IEvent
	if needDirectPublish {
		publishedEvents = make([]eventing.IEvent, 0, len(events))
	}

	for i, de := range events {
		if de == nil {
			return fmt.Errorf("domain event cannot be nil at index %d", i)
		}
		eventType := de.EventType()
		if eventType == "" {
			return fmt.Errorf("domain event type cannot be empty: %T", de)
		}
		version := currentVersion + uint64(i) + 1
		evt := eventing.NewDomainEvent(aggregateID, a.aggregateType, eventType, version, de)
		storableEvents = append(storableEvents, *evt)
		if needDirectPublish {
			publishedEvents = append(publishedEvents, evt)
		}
	}

	// Outbox 模式：通过 OutboxRepo 原子保存事件与 Outbox。
	if a.outboxRepo != nil {
		if err := a.outboxRepo.SaveWithEvents(ctx, aggregateID, storableEvents); err != nil {
			return err
		}
	} else {
		// 直接写入 EventStore。
		storable := eventing.ToStorable(storableEvents)
		if err := a.eventStore.AppendEvents(ctx, aggregateID, storable, expectedVersion); err != nil {
			return err
		}
	}

	// 可选事件发布（仅在非 Outbox 模式下直接发布）。
	if needDirectPublish {
		if err := a.eventBus.PublishEvents(ctx, publishedEvents); err != nil {
			// 发布失败视为可观测的业务失败，向上游返回错误并保留持久化结果
			return appErrors.WrapError(err, appErrors.ErrCodeDependency, "failed to publish domain events")
		}
	}

	return nil
}

// RestoreAggregate 根据快照与事件流恢复聚合。
func (a *DomainEventStore[T, ID]) RestoreAggregate(ctx context.Context, aggregate deventsourced.IEventSourcedAggregate[ID]) (uint64, error) {
	if aggregate == nil {
		return 0, fmt.Errorf("aggregate cannot be nil")
	}

	var fromVersion uint64

	// 若配置了 SnapshotManager，优先尝试通过快照 + 增量事件恢复：
	// 1) 通过 LoadSnapshot 将聚合状态恢复到快照版本；
	// 2) 记录快照版本，从该版本之后重放增量领域事件。
	if a.snapshotManager != nil {
		snap, err := a.snapshotManager.LoadSnapshot(ctx, aggregate.GetID(), aggregate)
		if err == nil && snap != nil {
			fromVersion = snap.Version
			a.logger.Debug(ctx, "aggregate restored from snapshot",
				logging.Any("aggregate_id", aggregate.GetID()),
				logging.Uint64("snapshot_version", snap.Version))
		} else if err != nil {
			a.logger.Info(ctx, "snapshot load failed, falling back to full replay",
				logging.Any("aggregate_id", aggregate.GetID()),
				logging.Error(err))
		}
	}

	// 加载剩余事件并应用为领域事件。
	var (
		events []eventing.Event[ID]
		err    error
	)

	if typedStore, ok := a.eventStore.(store.ITypedEventStore[ID]); ok {
		events, err = typedStore.LoadEventsByType(ctx, a.aggregateType, aggregate.GetID(), fromVersion)
	} else {
		events, err = a.eventStore.LoadEvents(ctx, aggregate.GetID(), fromVersion)
	}

	if err != nil {
		if errors.Is(err, eventing.ErrAggregateNotFound()) {
			// 不存在聚合时返回空聚合与版本 0。
			return 0, nil
		}
		return 0, err
	}

	for i := range events {
		evt := events[i]
		if _, err := eventing.UpgradeEventPayload(ctx, &evt); err != nil {
			a.logger.Debug(ctx, "event payload upgrade/hydrate failed",
				logging.String("event_type", evt.GetType()),
				logging.Error(err))
		}
		payload := evt.GetPayload()
		domainEvt, ok := payload.(domain.IDomainEvent)
		if !ok {
			return fromVersion, fmt.Errorf("event payload does not implement IDomainEvent: %T", payload)
		}
		if err := aggregate.ApplyEvent(domainEvt); err != nil {
			return fromVersion, fmt.Errorf("apply event failed: %w", err)
		}
	}
	aggregate.MarkEventsAsCommitted()

	// 汇总版本：若有新事件则取最后一条事件版本，否则保留 fromVersion。
	if len(events) > 0 {
		return events[len(events)-1].Version, nil
	}
	return fromVersion, nil
}

// Exists 检查聚合是否存在。
func (a *DomainEventStore[T, ID]) Exists(ctx context.Context, aggregateID ID) (bool, error) {
	if inspector, ok := a.eventStore.(store.IAggregateInspector[ID]); ok {
		return inspector.HasAggregate(ctx, aggregateID)
	}
	events, err := a.eventStore.LoadEvents(ctx, aggregateID, 0)
	if err != nil {
		if errors.Is(err, eventing.ErrAggregateNotFound()) {
			return false, nil
		}
		return false, err
	}
	return len(events) > 0, nil
}

// GetAggregateVersion 获取聚合当前版本。
func (a *DomainEventStore[T, ID]) GetAggregateVersion(ctx context.Context, aggregateID ID) (uint64, error) {
	if inspector, ok := a.eventStore.(store.IAggregateInspector[ID]); ok {
		return inspector.GetAggregateVersion(ctx, aggregateID)
	}
	events, err := a.eventStore.LoadEvents(ctx, aggregateID, 0)
	if err != nil {
		return 0, err
	}
	if len(events) == 0 {
		return 0, nil
	}
	return events[len(events)-1].Version, nil
}
