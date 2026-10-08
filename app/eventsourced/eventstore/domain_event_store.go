package eventstore

import (
	"strings"

	"gochen/app/internal/depcheck"
	"gochen/domain/eventsourced"
	"gochen/errors"
	"gochen/eventing/bus"
	"gochen/eventing/outbox"
	"gochen/eventing/registry"
	"gochen/eventing/upcast"
	"gochen/gen"
	"gochen/observe/logging"
)

// DomainEventStoreOptions 定义Domain事件存储可选项。
type DomainEventStoreOptions[ID comparable] struct {
	AggregateType string
	// EventIDGenerator 为每个持久化事件分配全局唯一 ID，由组合根显式注入。
	EventIDGenerator gen.IGenerator[string]

	// EventStore 提供聚合追加、版本读取与分页恢复能力（必填）。
	EventStore IAggregateEventStore[ID]

	// SnapshotManager 仅负责恢复阶段读取快照；保存后创建快照由 snapshottingRepository 负责。
	SnapshotManager ISnapshotLoader[ID]

	// PublishMode 必须显式选择持久化、直接发布或 Outbox；构造时校验模式与依赖一致。
	PublishMode PublishMode

	// EventBus 仅供 PublishModeDirect 使用。
	EventBus bus.IEventBus

	// OutboxRepo 仅供 PublishModeOutbox 使用；发布由独立的后台 Publisher 负责。
	OutboxRepo outbox.IOutboxRepository[ID]

	// EventRegistry/UpgraderRegistry 用于事件载荷升级与恢复，必须由组合根显式注入。
	EventRegistry    *registry.Registry
	UpgraderRegistry *upcast.UpgraderRegistry

	Logger logging.ILogger
}

// domainEventStore 适配领域事件与持久化事件流。
type domainEventStore[ID comparable] struct {
	aggregateType    string
	eventIDGenerator gen.IGenerator[string]
	eventStore       IAggregateEventStore[ID]
	snapshotManager  ISnapshotLoader[ID]
	eventBus         bus.IEventBus
	outboxRepo       outbox.IOutboxRepository[ID]
	publishMode      PublishMode
	logger           logging.ILogger

	eventRegistry *registry.Registry
	upgraders     *upcast.UpgraderRegistry
}

// NewDomainEventStore 创建事件流到领域存储的适配器。
func NewDomainEventStore[ID comparable](
	opts DomainEventStoreOptions[ID],
) (eventsourced.IDomainEventStore[ID], error) {
	base, err := newDomainEventStore(opts)
	if err != nil {
		return nil, err
	}
	return base, nil
}

func newDomainEventStore[ID comparable](opts DomainEventStoreOptions[ID]) (*domainEventStore[ID], error) {
	aggregateType := strings.TrimSpace(opts.AggregateType)
	if aggregateType == "" {
		return nil, errors.NewCode(errors.InvalidInput, "aggregate type cannot be empty")
	}
	if depcheck.IsNil(opts.EventStore) {
		return nil, errors.NewCode(errors.InvalidInput, "aggregate event store cannot be nil")
	}
	eventIDGenerator := opts.EventIDGenerator
	if depcheck.IsNil(eventIDGenerator) {
		return nil, errors.NewCode(errors.InvalidInput, "event ID generator cannot be nil")
	}

	if err := opts.validatePublishMode(); err != nil {
		return nil, err
	}

	adapter := &domainEventStore[ID]{
		aggregateType:    aggregateType,
		eventIDGenerator: eventIDGenerator,
		eventStore:       opts.EventStore,
		snapshotManager:  opts.SnapshotManager,
		eventBus:         opts.EventBus,
		outboxRepo:       opts.OutboxRepo,
		publishMode:      opts.PublishMode,
		logger:           opts.Logger,
		eventRegistry:    opts.EventRegistry,
		upgraders:        opts.UpgraderRegistry,
	}
	if depcheck.IsNil(adapter.logger) {
		adapter.logger = logging.ComponentLogger("app.eventsourced.domain_event_store").
			WithField("aggregate_type", aggregateType)
	}
	if adapter.eventRegistry == nil {
		return nil, errors.NewCode(errors.InvalidInput, "event registry cannot be nil")
	}
	if adapter.upgraders == nil {
		return nil, errors.NewCode(errors.InvalidInput, "event upgrader registry cannot be nil")
	}

	return adapter, nil
}
