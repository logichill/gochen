package eventstore

import (
	"context"

	"gochen/app/internal/depcheck"
	deventsourced "gochen/domain/eventsourced"
	"gochen/errors"
	"gochen/eventing/store"
)

// atomicDomainEventStore 仅对纯持久化且支持原子追加的存储暴露批量能力。
type atomicDomainEventStore[ID comparable] struct {
	*domainEventStore[ID]
	atomicStore store.IAtomicEventStore[ID]
}

// NewAtomicDomainEventStore 在装配期校验原子能力；不支持的组合不会生成可用实例。
func NewAtomicDomainEventStore[ID comparable](opts DomainEventStoreOptions[ID]) (deventsourced.IAtomicDomainEventStore[ID], error) {
	base, err := newDomainEventStore(opts)
	if err != nil {
		return nil, err
	}
	if opts.PublishMode != PublishModePersistOnly {
		return nil, errors.NewCode(errors.Unsupported, "atomic append requires persistence-only configuration")
	}
	atomicStore, ok := opts.EventStore.(store.IAtomicEventStore[ID])
	if !ok || depcheck.IsNil(atomicStore) {
		return nil, errors.NewCode(errors.Unsupported, "event store does not support atomic append")
	}
	return &atomicDomainEventStore[ID]{domainEventStore: base, atomicStore: atomicStore}, nil
}

// AppendEventsBatch 原子追加多个聚合的领域事件。
// 批量模式仅支持不带 Outbox 和直接发布的存储配置，避免部分发布或静默跳过发布。
func (a *atomicDomainEventStore[ID]) AppendEventsBatch(ctx context.Context, batch []deventsourced.DomainEventAppend[ID]) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(batch) == 0 {
		return nil
	}
	requests := make([]store.EventAppend[ID], 0, len(batch))
	for _, entry := range batch {
		events, err := a.prepareDomainEvents(ctx, entry.AggregateID, entry.Events, entry.ExpectedVersion)
		if err != nil {
			return err
		}
		requests = append(requests, store.EventAppend[ID]{AggregateType: a.aggregateType, AggregateID: entry.AggregateID, Events: store.ToStorable(events), ExpectedVersion: entry.ExpectedVersion})
	}
	return a.atomicStore.AppendEventsBatch(ctx, requests)
}
