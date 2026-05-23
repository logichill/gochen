package eventsourced

import (
	"context"
	"fmt"
	"gochen/contextx"
	"gochen/domain"
	deventsourced "gochen/domain/eventsourced"
	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/bus"
	"gochen/eventing/outbox"
	"gochen/eventing/registry"
	"gochen/eventing/store"
	"gochen/eventing/store/snapshot"
	"gochen/eventing/upcast"
	"gochen/logging"
	"reflect"
)

// DomainEventStoreOptions 定义Domain事件存储可选项。
type DomainEventStoreOptions[T deventsourced.IEventSourcedAggregate[ID], ID comparable] struct {
	AggregateType string

	// EventStore 事件存储（必填），负责 Append/Load/Exists/GetVersion 以及可选的 StreamEvents/StreamAggregate。
	EventStore store.IEventStreamStore[ID]

	SnapshotManager *snapshot.Manager[ID]
	EventBus        bus.IEventBus
	OutboxRepo      outbox.IOutboxRepository[ID]
	PublishEvents   bool

	// Registry/UpgraderRegistry 用于事件载荷 upgrade/hydration（建议在组合根显式注入，以消除全局依赖）。
	EventRegistry    *registry.Registry
	UpgraderRegistry *upcast.UpgraderRegistry

	Logger logging.ILogger
}

// DomainEventStore 定义Domain事件存储。
type DomainEventStore[T deventsourced.IEventSourcedAggregate[ID], ID comparable] struct {
	aggregateType   string
	eventStore      store.IEventStreamStore[ID]
	snapshotManager *snapshot.Manager[ID]
	eventBus        bus.IEventBus
	outboxRepo      outbox.IOutboxRepository[ID]
	publishEvents   bool
	logger          logging.ILogger

	eventRegistry *registry.Registry
	upgraders     *upcast.UpgraderRegistry
}

const restoreAggregateBatchLimit = 1000

// NewDomainEventStore 创建Domain事件存储。
func NewDomainEventStore[T deventsourced.IEventSourcedAggregate[ID], ID comparable](
	opts DomainEventStoreOptions[T, ID],
) (deventsourced.IDomainEventStore[ID], error) {
	if opts.AggregateType == "" {
		return nil, errors.NewCode(errors.InvalidInput, "aggregate type cannot be empty")
	}
	if opts.EventStore == nil {
		return nil, errors.NewCode(errors.InvalidInput, "aggregate event store cannot be nil")
	}

	adapter := &DomainEventStore[T, ID]{
		aggregateType:   opts.AggregateType,
		eventStore:      opts.EventStore,
		snapshotManager: opts.SnapshotManager,
		eventBus:        opts.EventBus,
		outboxRepo:      opts.OutboxRepo,
		publishEvents:   opts.PublishEvents,
		logger:          opts.Logger,
		eventRegistry:   opts.EventRegistry,
		upgraders:       opts.UpgraderRegistry,
	}
	if adapter.logger == nil {
		adapter.logger = logging.ComponentLogger("app.eventsourced.domain_event_store").
			WithField("aggregate_type", opts.AggregateType)
	}
	if adapter.eventRegistry == nil {
		return nil, errors.NewCode(errors.InvalidInput, "event registry cannot be nil")
	}
	if adapter.upgraders == nil {
		return nil, errors.NewCode(errors.InvalidInput, "event upgrader registry cannot be nil")
	}

	// 配置提示：启用 PublishEvents 但未配置 OutboxRepo 时，会退化为“直接发布”模式，
	// 该模式不具备“事件持久化 + 发布”的原子性；生产环境建议启用 Outbox。
	if opts.PublishEvents && opts.EventBus != nil && opts.OutboxRepo == nil {
		adapter.logger.Warn(contextx.Background(), "PublishEvents=true but OutboxRepo is nil; falling back to direct publish mode (non-atomic)")
	}

	return adapter, nil
}

// AppendEvents 将领域事件追加到事件存储中。
//
// 说明：
// - 支持两种模式：
// - - 配置 OutboxRepo 时：在同一数据库事务中同时写入事件与 Outbox 记录，保证原子性；
// - - 未配置 OutboxRepo 时：先直接调用 EventStore.AppendEvents 持久化事件，再尝试通过 EventBus 发布事件。
// - ⚠️ 注意：未启用 Outbox 的模式存在原子性风险。
// - 当 PublishEvents=true 且 OutboxRepo=nil 时，事件持久化与发布是两个独立步骤：
// - 1. EventStore.AppendEvents(ctx, ...)  // 事件写入成功。
// - 2. EventBus.Publish(ctx, ...)         // 发布可能失败。
// - 如果第 1 步成功而第 2 步失败：
// - - 事件已经持久化，但消息总线未收到通知；
// - - 下游系统不会感知到这批事件；
// - - 需要额外补偿机制或通过重放/扫描来补发事件。
// - 在生产环境强烈建议使用 Outbox 模式，以保证“最终一致”的语义。
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
			return errors.NewCode(errors.InvalidInput, "domain event cannot be nil").
				WithContext("index", i)
		}
		eventType := de.EventType()
		if eventType == "" {
			return errors.NewCode(errors.InvalidInput, "domain event type cannot be empty").
				WithContext("domain_event_type", fmt.Sprintf("%T", de))
		}
		if !a.eventRegistry.HasEvent(eventType) {
			return errors.NewCode(errors.NotFound, "unknown event type").
				WithContext("event_type", eventType).
				WithContext("aggregate_type", a.aggregateType).
				WithContext("aggregate_id", aggregateID).
				WithContext("index", i)
		}
		schemaVersion := a.eventRegistry.EventSchemaVersion(eventType)
		version := currentVersion + uint64(i) + 1
		evt := eventing.NewEvent(aggregateID, a.aggregateType, eventType, version, de, schemaVersion)
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
		storable := store.ToStorable(storableEvents)
		if err := a.eventStore.AppendEvents(ctx, aggregateID, storable, expectedVersion); err != nil {
			return err
		}
	}

	// 可选事件发布（仅在非 Outbox 模式下直接发布）。
	if needDirectPublish {
		if err := a.eventBus.PublishEvents(ctx, publishedEvents); err != nil {
			// 发布失败视为可观测的业务失败，向上游返回错误并保留持久化结果
			return errors.Wrap(err, errors.Dependency, "failed to publish domain events")
		}
	}

	return nil
}

func validateAggregateSample(sample any, aggregateType string) error {
	if sample == nil {
		return errors.NewCode(errors.InvalidInput, "aggregate sample cannot be nil").
			WithContext("aggregate_type", aggregateType)
	}
	value := reflect.ValueOf(sample)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		if value.IsNil() {
			return errors.NewCode(errors.InvalidInput, "aggregate sample cannot be nil").
				WithContext("aggregate_type", aggregateType)
		}
	}
	return nil
}

// RestoreAggregate 根据快照与事件流恢复聚合。
//
// 说明：
// - 返回 RestoreResult 包含恢复详情。
//
// 参数：
// - aggregate：聚合实例。
func (a *DomainEventStore[T, ID]) RestoreAggregate(ctx context.Context, aggregate deventsourced.IEventSourcedAggregate[ID]) (*deventsourced.RestoreResult, error) {
	if aggregate == nil {
		return nil, errors.NewCode(errors.InvalidInput, "aggregate cannot be nil")
	}

	result := &deventsourced.RestoreResult{}
	var fromVersion uint64

	// 若配置了 SnapshotManager，优先尝试通过快照 + 增量事件恢复：
	// 1) 通过 LoadSnapshot 将聚合状态恢复到快照版本；
	// 2) 记录快照版本，从该版本之后重放增量领域事件。
	if a.snapshotManager != nil {
		snap, err := a.snapshotManager.LoadSnapshot(ctx, aggregate.GetID(), aggregate)
		if err == nil && snap != nil {
			fromVersion = snap.Version
			result.FromSnapshot = true
			result.SnapshotVersion = snap.Version

			// 显式设置聚合版本号（如果聚合支持 IVersionSettable 接口）
			if versioned, ok := aggregate.(deventsourced.IVersionSettable); ok {
				versioned.SetVersion(snap.Version)
			}

			a.logger.Debug(ctx, "aggregate restored from snapshot",
				logging.Any("aggregate_id", aggregate.GetID()),
				logging.Uint64("snapshot_version", snap.Version))
		} else if err != nil {
			a.logger.Info(ctx, "snapshot load failed, falling back to full replay",
				logging.Any("aggregate_id", aggregate.GetID()),
				logging.Error(err))
		}
	}

	applyOne := func(evt *eventing.Event[ID]) error {
		if evt == nil {
			return errors.NewCode(errors.InvalidInput, "event cannot be nil")
		}
		domainEvt, err := asDomainEvent(ctx, a.eventRegistry, a.upgraders, evt)
		if err != nil {
			return err
		}
		if err := aggregate.ApplyEvent(domainEvt); err != nil {
			var appErr *errors.AppError
			if errors.As(err, &appErr) && appErr != nil {
				return appErr.
					WithContext("event_type", evt.GetType()).
					WithContext("event_id", evt.GetID())
			}
			return errors.Wrap(err, errors.Internal, "apply event failed").
				WithContext("event_type", evt.GetType()).
				WithContext("event_id", evt.GetID())
		}
		return nil
	}

	// 使用流式接口分批读取，避免一次性加载大聚合事件流导致内存峰值/GC 压力。
	var lastVersion = fromVersion
	afterVersion := fromVersion
	for {
		batch, err := a.eventStore.StreamAggregate(ctx, &store.AggregateStreamOptions[ID]{
			AggregateType: a.aggregateType,
			AggregateID:   aggregate.GetID(),
			AfterVersion:  afterVersion,
			Limit:         restoreAggregateBatchLimit,
		})
		if err != nil {
			if errors.Is(err, errors.NotFound) {
				return result, nil
			}
			return nil, err
		}
		if batch == nil || len(batch.Events) == 0 {
			break
		}
		for i := range batch.Events {
			evt := &batch.Events[i]
			if err := applyOne(evt); err != nil {
				return nil, err
			}
			result.EventCount++
			lastVersion = evt.Version
		}
		if !batch.HasMore {
			break
		}
		// 使用 NextVersion 推进游标；若实现未提供 NextVersion，则退化为最后一条事件版本号。
		if batch.NextVersion > afterVersion {
			afterVersion = batch.NextVersion
		} else {
			afterVersion = lastVersion
		}
	}

	aggregate.MarkEventsAsCommitted()
	result.Version = lastVersion
	result.Exists = result.Version > 0
	return result, nil
}

// Exists 检查聚合是否存在。
func (a *DomainEventStore[T, ID]) Exists(ctx context.Context, aggregateID ID) (bool, error) {
	events, err := a.eventStore.LoadEventsByType(ctx, a.aggregateType, aggregateID, 0)
	if err != nil {
		if errors.Is(err, errors.NotFound) {
			return false, nil
		}
		return false, err
	}
	return len(events) > 0, nil
}

// GetAggregateVersion 从存储中查询对象。
//
// 说明：
// - GetAggregateVersion 获取聚合当前版本。
func (a *DomainEventStore[T, ID]) GetAggregateVersion(ctx context.Context, aggregateID ID) (uint64, error) {
	events, err := a.eventStore.LoadEventsByType(ctx, a.aggregateType, aggregateID, 0)
	if err != nil {
		if errors.Is(err, errors.NotFound) {
			return 0, nil
		}
		return 0, err
	}
	if len(events) == 0 {
		return 0, nil
	}
	return events[len(events)-1].Version, nil
}
