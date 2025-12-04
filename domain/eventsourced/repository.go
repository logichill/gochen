package eventsourced

import (
	"context"
	"errors"
	"fmt"

	"gochen/eventing"
	"gochen/eventing/bus"
	"gochen/eventing/store"
	"gochen/eventing/store/snapshot"
	"gochen/logging"
)

// EventSourcedRepositoryOptions 配置事件溯源仓储
type EventSourcedRepositoryOptions[T IEventSourcedAggregate[int64]] struct {
	AggregateType   string
	Factory         func(id int64) T
	EventStore      store.IEventStore
	SnapshotManager *snapshot.Manager
	EventBus        bus.IEventBus
	PublishEvents   bool
	Logger          logging.ILogger
}

// EventSourcedRepository 提供统一的事件溯源仓储实现
type EventSourcedRepository[T IEventSourcedAggregate[int64]] struct {
	aggregateType   string
	factory         func(id int64) T
	eventStore      store.IEventStore
	snapshotManager *snapshot.Manager
	eventBus        bus.IEventBus
	publishEvents   bool
	logger          logging.ILogger
}

// NewEventSourcedRepository 创建新的事件溯源仓储模板
func NewEventSourcedRepository[T IEventSourcedAggregate[int64]](opts EventSourcedRepositoryOptions[T]) (*EventSourcedRepository[T], error) {
	if opts.AggregateType == "" {
		return nil, fmt.Errorf("aggregate type cannot be empty")
	}
	if opts.Factory == nil {
		return nil, fmt.Errorf("aggregate factory cannot be nil")
	}
	if opts.EventStore == nil {
		return nil, fmt.Errorf("event store cannot be nil")
	}
	repo := &EventSourcedRepository[T]{
		aggregateType:   opts.AggregateType,
		factory:         opts.Factory,
		eventStore:      opts.EventStore,
		snapshotManager: opts.SnapshotManager,
		eventBus:        opts.EventBus,
		publishEvents:   opts.PublishEvents,
		logger:          opts.Logger,
	}
	if repo.logger == nil {
		repo.logger = logging.ComponentLogger("eventsourced.repository").
			WithField("aggregate_type", opts.AggregateType)
	}
	return repo, nil
}

// NewAggregate 使用工厂创建聚合实例
func (r *EventSourcedRepository[T]) NewAggregate(id int64) T {
	return r.factory(id)
}

// Save 持久化聚合的未提交事件
func (r *EventSourcedRepository[T]) Save(ctx context.Context, aggregate T) error {
	events := aggregate.GetUncommittedEvents()
	if len(events) == 0 {
		return nil
	}

	aggregateID := aggregate.GetID()
	currentVersion := uint64(aggregate.GetVersion())
	var expectedVersion uint64
	if currentVersion >= uint64(len(events)) {
		expectedVersion = currentVersion - uint64(len(events))
	}

	storableEvents := make([]eventing.IStorableEvent, len(events))
	for i, evt := range events {
		storable, ok := evt.(eventing.IStorableEvent)
		if !ok {
			return fmt.Errorf("event does not implement IStorableEvent: %T", evt)
		}
		if storable.GetAggregateType() == "" {
			storable.SetAggregateType(r.aggregateType)
		}
		storableEvents[i] = storable
	}

	if err := r.eventStore.AppendEvents(ctx, aggregateID, storableEvents, expectedVersion); err != nil {
		return err
	}

	aggregate.MarkEventsAsCommitted()

	if r.snapshotManager != nil {
		if should, err := r.snapshotManager.ShouldCreateSnapshot(ctx, snapshotAggregateAdapter[T]{aggregate: aggregate}); err == nil && should {
			if err := r.snapshotManager.CreateSnapshot(ctx, aggregateID, r.aggregateType, aggregate, currentVersion); err != nil {
				r.logger.Warn(ctx, "failed to create snapshot",
					logging.Error(err),
					logging.Int64("aggregate_id", aggregateID),
					logging.Uint64("version", currentVersion))
			}
		}
	}

	if r.publishEvents && r.eventBus != nil {
		if err := r.eventBus.PublishEvents(ctx, events); err != nil {
			r.logger.Warn(ctx, "failed to publish events", logging.Error(err), logging.Int64("aggregate_id", aggregateID))
		}
	}

	return nil
}

// GetByID 加载聚合（从快照和事件恢复）
func (r *EventSourcedRepository[T]) GetByID(ctx context.Context, id int64) (T, error) {
	aggregate := r.NewAggregate(id)

	var fromVersion uint64
	if r.snapshotManager != nil {
		version, err := r.snapshotManager.RestoreAggregate(ctx, id, aggregate)
		if err == nil {
			fromVersion = version
			r.logger.Debug(ctx, "aggregate restored from snapshot",
				logging.Int64("aggregate_id", id),
				logging.Uint64("snapshot_version", version))
		}
	}

	var (
		events []eventing.Event
		err    error
	)

	if typedStore, ok := r.eventStore.(store.ITypedEventStore); ok {
		events, err = typedStore.LoadEventsByType(ctx, r.aggregateType, id, fromVersion)
	} else {
		events, err = r.eventStore.LoadEvents(ctx, id, fromVersion)
	}

	if err != nil {
		if errors.Is(err, eventing.ErrAggregateNotFound) {
			return aggregate, nil
		}
		return aggregate, err
	}

	for i := range events {
		evt := events[i]
		if _, err := eventing.UpgradeEventPayload(ctx, &evt); err != nil {
			r.logger.Debug(ctx, "event payload upgrade/hydrate failed",
				logging.String("event_type", evt.GetType()), logging.Error(err))
		}
		if err := aggregate.ApplyEvent(&evt); err != nil {
			return aggregate, fmt.Errorf("apply event failed: %w", err)
		}
	}
	aggregate.MarkEventsAsCommitted()
	return aggregate, nil
}

// Exists 判断聚合是否存在
func (r *EventSourcedRepository[T]) Exists(ctx context.Context, id int64) (bool, error) {
	if inspector, ok := r.eventStore.(store.IAggregateInspector); ok {
		return inspector.HasAggregate(ctx, id)
	}

	events, err := r.eventStore.LoadEvents(ctx, id, 0)
	if err != nil {
		if errors.Is(err, eventing.ErrAggregateNotFound) {
			return false, nil
		}
		return false, err
	}
	return len(events) > 0, nil
}

// GetVersion 获取聚合当前版本
func (r *EventSourcedRepository[T]) GetAggregateVersion(ctx context.Context, id int64) (uint64, error) {
	if inspector, ok := r.eventStore.(store.IAggregateInspector); ok {
		return inspector.GetAggregateVersion(ctx, id)
	}
	events, err := r.eventStore.LoadEvents(ctx, id, 0)
	if err != nil {
		return 0, err
	}
	if len(events) == 0 {
		return 0, nil
	}
	return events[len(events)-1].Version, nil
}
