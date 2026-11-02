package eventsourced

import (
	"context"

	"gochen/app/eventsourced/internal/aggregatecheck"
	"gochen/contextx"
	deventsourced "gochen/domain/eventsourced"
	"gochen/errors"
	"gochen/internal/depcheck"
)

// atomicEventSourcedRepository 为已校验的原子领域存储提供多聚合保存。
// 普通 EventSourcedRepository 只暴露单项保存能力。
type atomicEventSourcedRepository[T deventsourced.IEventSourcedAggregate[ID], ID comparable] struct {
	*EventSourcedRepository[T, ID]
	atomicStore deventsourced.IAtomicDomainEventStore[ID]
}

// NewAtomicEventSourcedRepository 在构造时要求已支持原子追加的领域存储。
func NewAtomicEventSourcedRepository[T deventsourced.IEventSourcedAggregate[ID], ID comparable](opts RepositoryOptions[T, ID]) (deventsourced.IAtomicEventSourcedRepository[T, ID], error) {
	if depcheck.IsNil(opts.Store) {
		return nil, errors.NewCode(errors.InvalidInput, "event store cannot be nil")
	}
	atomicStore, ok := opts.Store.(deventsourced.IAtomicDomainEventStore[ID])
	if !ok || depcheck.IsNil(atomicStore) {
		return nil, errors.NewCode(errors.Unsupported, "domain event store does not support atomic append")
	}
	base, err := NewEventSourcedRepository(opts)
	if err != nil {
		return nil, err
	}
	return &atomicEventSourcedRepository[T, ID]{EventSourcedRepository: base, atomicStore: atomicStore}, nil
}

// SaveAll 将多个聚合的待提交事件原子保存，成功后才清除各聚合的待提交事件。
// 没有待提交事件的聚合作为严格版本守卫，用于校验业务决策依赖的读取。
func (r *atomicEventSourcedRepository[T, ID]) SaveAll(ctx context.Context, aggregates []T) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	batch := make([]deventsourced.DomainEventAppend[ID], 0, len(aggregates))
	seen := make(map[ID]bool, len(aggregates))
	for _, aggregate := range aggregates {
		if err := aggregatecheck.ValidateInstance(aggregate, r.aggregateType); err != nil {
			return err
		}
		id := aggregate.GetID()
		if seen[id] {
			return errors.NewCode(errors.InvalidInput, "duplicate aggregate in atomic save")
		}
		seen[id] = true
		batch = append(batch, deventsourced.DomainEventAppend[ID]{AggregateID: id, Events: aggregate.GetUncommittedEvents(), ExpectedVersion: aggregate.GetExpectedVersion()})
	}
	if len(batch) == 0 {
		return nil
	}
	err := r.atomicStore.AppendEventsBatch(ctx, batch)
	if err != nil && !contextx.IsAfterCommitError(err) {
		return err
	}
	for _, aggregate := range aggregates {
		aggregate.MarkEventsAsCommitted()
	}
	return err
}
