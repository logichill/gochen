package eventsourced

import (
	"context"

	deventsourced "gochen/domain/eventsourced"
	"gochen/errors"
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
// 具体的事件存储/快照/Outbox/EventBus 等能力由上层通过 IEventStore 适配器提供（例如 app/eventsourced.DomainEventStore）。
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

// NewEventSourcedRepository 创建事件Sourced仓储。
func NewEventSourcedRepository[T deventsourced.IEventSourcedAggregate[ID], ID comparable](
	opts RepositoryOptions[T, ID],
) (*EventSourcedRepository[T, ID], error) {
	aggregateType := opts.AggregateType
	if aggregateType == "" {
		return nil, errors.NewCode(errors.InvalidInput, "aggregate type cannot be empty")
	}
	if err := validateAggregateSample(opts.Sample, aggregateType); err != nil {
		return nil, err
	}
	if opts.Factory == nil {
		return nil, errors.NewCode(errors.InvalidInput, "aggregate factory cannot be nil")
	}
	if opts.Store == nil {
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

// Save 持久化聚合上的未提交事件。
//
// 说明：
// - 仓储使用聚合显式暴露的 `GetExpectedVersion()` 作为乐观锁基线版本，
// - 不再通过“当前版本号 - 未提交事件数量”做反推。
func (r *EventSourcedRepository[T, ID]) Save(ctx context.Context, aggregate T) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	events := aggregate.GetUncommittedEvents()
	if len(events) == 0 {
		return nil
	}

	if err := r.store.AppendEvents(ctx, aggregate.GetID(), events, aggregate.GetExpectedVersion()); err != nil {
		return err
	}

	aggregate.MarkEventsAsCommitted()
	return nil
}

// Get 从存储中查询对象。
//
// 说明：
// - Get 根据 ID 加载聚合（通过 RestoreAggregate 恢复）。
// - 若聚合不存在，返回 errors.NotFound。
// - factory 创建的聚合实例会在仓储侧显式绑定预编译元数据（若支持）。
func (r *EventSourcedRepository[T, ID]) Get(ctx context.Context, id ID) (T, error) {
	if ctx == nil {
		var zero T
		return zero, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	aggregate, err := r.newAggregate(id)
	if err != nil {
		var zero T
		return zero, err
	}
	result, err := r.store.RestoreAggregate(ctx, aggregate)
	if err != nil {
		return aggregate, err
	}
	if !result.Exists {
		var zero T
		return zero, errors.NewCode(errors.NotFound, "aggregate not found").
			WithContext("aggregate_type", r.aggregateType).
			WithContext("id", id)
	}
	return aggregate, nil
}

// GetOrCreate 从存储中查询对象。
//
// 说明：
// - GetOrCreate 获取或创建聚合。
// - 若聚合存在则返回已有状态；若不存在则返回 factory 创建的新实例（version=0）。
// - 适用于"幂等创建"场景，调用方可通过 aggregate.GetVersion() == 0 判断是否为新建。
func (r *EventSourcedRepository[T, ID]) GetOrCreate(ctx context.Context, id ID) (T, error) {
	if ctx == nil {
		var zero T
		return zero, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	aggregate, err := r.newAggregate(id)
	if err != nil {
		var zero T
		return zero, err
	}
	_, err = r.store.RestoreAggregate(ctx, aggregate)
	if err != nil {
		return aggregate, err
	}
	return aggregate, nil
}

// Exists 检查聚合是否存在。
func (r *EventSourcedRepository[T, ID]) Exists(ctx context.Context, id ID) (bool, error) {
	if ctx == nil {
		return false, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	return r.store.Exists(ctx, id)
}

// GetAggregateVersion 从存储中查询对象。
//
// 说明：
// - GetAggregateVersion 获取聚合当前版本。
func (r *EventSourcedRepository[T, ID]) GetAggregateVersion(ctx context.Context, id ID) (uint64, error) {
	if ctx == nil {
		return 0, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	version, err := r.store.GetAggregateVersion(ctx, id)
	if err != nil {
		return 0, err
	}
	// 语义约定：不存在返回 0。
	return version, nil
}

// Ensure interface compliance.
var _ deventsourced.IEventSourcedRepository[deventsourced.IEventSourcedAggregate[int64], int64] = (*EventSourcedRepository[deventsourced.IEventSourcedAggregate[int64], int64])(nil)
