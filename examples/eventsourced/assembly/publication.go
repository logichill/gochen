package main

import (
	"context"
	"fmt"
	esapp "gochen/app/eventsourced"
	esstore "gochen/app/eventsourced/eventstore"
	"gochen/contextx"
	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/bus"
	"gochen/eventing/outbox"
	"gochen/eventing/upcast"
	"gochen/observe/logging"
	"time"
)

// recordingBus 模拟部分发布后报错；用事件 ID 去重代表消费者的幂等责任。
type recordingBus struct {
	bus.IEventBus
	fail      bool
	delivered map[string]bool
}

func (b *recordingBus) PublishEvent(ctx context.Context, e eventing.IEvent) error {
	return b.PublishEvents(ctx, []eventing.IEvent{e})
}
func (b *recordingBus) PublishEvents(_ context.Context, events []eventing.IEvent) error {
	for _, e := range events {
		b.delivered[e.GetID()] = true
	}
	if b.fail {
		return errors.NewCode(errors.Dependency, "delivery acknowledgement lost")
	}
	return nil
}

func recoverPublication() {
	s := newAssembly()
	b := &recordingBus{fail: true, delivered: map[string]bool{}}
	opts := s.options(esstore.PublishModeDirect)
	opts.EventBus = b
	storage, err := esstore.NewDomainEventStore(opts)
	must(err)
	repo, err := esapp.NewEventSourcedRepository(s.repositoryOptions(storage))
	must(err)
	a := s.aggregate(1)
	before := a.GetExpectedVersion()
	must(a.ApplyAndRecord(&balanceChanged{10}))
	through := a.GetVersion()
	err = repo.Save(context.Background(), a)
	check(contextx.IsAfterCommitError(err) && len(a.GetUncommittedEvents()) == 0, "publication failure lost commit state")

	// 恢复任务必须保留成功提交的版本范围。只补发原存储信封，不重跑 Save 或生成新事件 ID。
	recoveryCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	persisted, err := s.events.LoadEvents(recoveryCtx, "Account", 1, before)
	must(err)
	b.fail = false
	for i := range persisted {
		e := &persisted[i]
		if e.Version > through {
			break
		}
		_, err := upcast.UpgradeEventPayload(recoveryCtx, s.registry, s.upgraders, e)
		must(err)
		must(b.PublishEvent(recoveryCtx, e))
	}
	version, err := s.events.GetAggregateVersion(recoveryCtx, "Account", 1)
	must(err)
	check(version == through && len(b.delivered) == 1, "recovery created duplicate business events")
	fmt.Println("direct publication recovery: original event IDs, no repeated business write")
}

func publishOutbox() {
	ctx := context.Background()
	s := newAssembly()
	ob, err := outbox.NewMemoryOutboxRepository(s.events)
	must(err)
	opts := s.options(esstore.PublishModeOutbox)
	opts.OutboxRepo = ob
	storage, err := esstore.NewDomainEventStore(opts)
	must(err)
	repo, err := esapp.NewEventSourcedRepository(s.repositoryOptions(storage))
	must(err)
	a := s.aggregate(1)
	must(a.ApplyAndRecord(&balanceChanged{8}))
	must(repo.Save(ctx, a))
	b := &recordingBus{delivered: map[string]bool{}}
	publisher, err := outbox.NewPublisher[int64](ob, b, outbox.DefaultOutboxConfig(), logging.NewNoopLogger(), s.registry, s.upgraders)
	must(err)
	must(publisher.PublishPending(ctx))
	must(publisher.PublishPending(ctx))
	check(len(b.delivered) == 1, "outbox did not publish persisted event")
	fmt.Println("outbox: atomic enqueue and independent publication")
}
