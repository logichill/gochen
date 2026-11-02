package eventsourced

import (
	"strings"

	"gochen/app/eventsourced/internal/aggregatecheck"
	deventsourced "gochen/domain/eventsourced"
	"gochen/errors"
	"gochen/internal/depcheck"
)

// RepositoryOptions 定义事件溯源仓储装配选项。
type RepositoryOptions[T deventsourced.IEventSourcedAggregate[ID], ID comparable] struct {
	AggregateType    string
	Sample           T
	Factory          func(id ID) (T, error)
	Store            deventsourced.IDomainEventStore[ID]
	MetadataRegistry *deventsourced.MetadataRegistry
}

// EventSourcedRepository 默认事件溯源仓储实现（应用层提供）。
//
// 该实现仅依赖领域抽象：
//   - domain/eventsourced.IEventSourcedAggregate[ID]：聚合根；
//   - domain/eventsourced.IDomainEventStore[ID]：领域事件存储接口。
//
// 具体的事件存储/快照/Outbox/EventBus 等能力由上层通过 IEventStore 适配器提供（由 app/eventsourced/eventstore.NewDomainEventStore 构造）。
//
// 反射支持（复用 domain/eventsourced 的元数据机制）：
//   - metadata 优先复用装配期通过 MetadataRegistry / 模块 Aggregates 预注册的结果；
//   - 若调用方未显式预注册，仓储构造时会通过显式 sample 自动 ensure metadata；
//   - 仓储加载/创建聚合时显式把预编译 metadata 绑定到聚合实例；
//   - 运行时主路径只消费预编译 metadata，不再重复反射扫描。
//
// 类型参数：
//   - T: 聚合根类型。
//   - ID: 聚合根 ID 类型，必须是可比较类型。
type EventSourcedRepository[T deventsourced.IEventSourcedAggregate[ID], ID comparable] struct {
	aggregateType string
	// factory 聚合根回放工厂函数。
	// 用途：运行时创建空实例用于回放事件。
	// 采用 error-return 形式，避免回放路径被迫依赖 panic。
	factory  func(id ID) (T, error)
	store    deventsourced.IDomainEventStore[ID]
	metadata *deventsourced.Metadata
}

// NewEventSourcedRepository 校验依赖并预编译聚合元数据。
func NewEventSourcedRepository[T deventsourced.IEventSourcedAggregate[ID], ID comparable](
	opts RepositoryOptions[T, ID],
) (*EventSourcedRepository[T, ID], error) {
	aggregateType := strings.TrimSpace(opts.AggregateType)
	if aggregateType == "" {
		return nil, errors.NewCode(errors.InvalidInput, "aggregate type cannot be empty")
	}
	if err := aggregatecheck.ValidateSample(opts.Sample, aggregateType); err != nil {
		return nil, err
	}
	if opts.Factory == nil {
		return nil, errors.NewCode(errors.InvalidInput, "aggregate factory cannot be nil")
	}
	if depcheck.IsNil(opts.Store) {
		return nil, errors.NewCode(errors.InvalidInput, "event store cannot be nil")
	}
	if opts.MetadataRegistry == nil {
		return nil, errors.NewCode(errors.InvalidInput, "metadata registry cannot be nil")
	}

	// 构造时基于显式 sample 获取聚合 metadata，避免运行期回放工厂承担启动期预热职责。
	var err error
	metadata, err := opts.MetadataRegistry.Ensure(opts.Sample, aggregateType)
	if err != nil {
		return nil, err
	}

	return &EventSourcedRepository[T, ID]{
		aggregateType: aggregateType,
		factory:       opts.Factory,
		store:         opts.Store,
		metadata:      metadata,
	}, nil
}

// newAggregate 创建聚合。
func (r *EventSourcedRepository[T, ID]) newAggregate(id ID) (T, error) {
	aggregate, err := r.factory(id)
	if err != nil {
		var zero T
		return zero, err
	}
	if err := aggregatecheck.ValidateInstance(aggregate, r.aggregateType); err != nil {
		var zero T
		return zero, err
	}
	if binder, ok := any(aggregate).(interface {
		BindMetadata(any, *deventsourced.Metadata) error
	}); ok && r.metadata != nil {
		if err := binder.BindMetadata(aggregate, r.metadata); err != nil {
			var zero T
			return zero, err
		}
	}
	return aggregate, nil
}

// Ensure interface compliance.
var _ deventsourced.IEventSourcedRepository[deventsourced.IEventSourcedAggregate[int64], int64] = (*EventSourcedRepository[deventsourced.IEventSourcedAggregate[int64], int64])(nil)
