package eventsourced

import (
	"context"
	esstore "gochen/app/eventsourced/eventstore"
	"testing"

	"gochen/contextx"
	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/bus"
	"gochen/eventing/store"
)

type failingPublishBus struct {
	bus.IEventBus
	calls int
}

func (b *failingPublishBus) PublishEvents(context.Context, []eventing.IEvent) error {
	b.calls++
	return errors.NewCode(errors.Dependency, "publish unavailable")
}

func TestRepositoryCommitsAggregateWhenDirectPublicationFails(t *testing.T) {
	ctx := context.Background()
	events := store.NewMemoryEventStore[int64]()
	reg, upgraders := newTestRegistryAndUpgraders()
	if err := reg.Register("ValueSet", func() any { return &valueSetEvent{} }); err != nil {
		t.Fatal(err)
	}
	publisher := &failingPublishBus{}
	adapter, err := esstore.NewDomainEventStore(esstore.DomainEventStoreOptions[int64]{
		PublishMode: esstore.PublishModeDirect, AggregateType: "TestAggregate", EventIDGenerator: testEventIDGenerator(), EventStore: events, EventBus: publisher, EventRegistry: reg, UpgraderRegistry: upgraders})
	if err != nil {
		t.Fatal(err)
	}
	repo, err := newTestEventSourcedRepository[*testAggregate, int64]("TestAggregate", &testAggregate{}, AdaptAggregateFactory(newTestAggregate), adapter)
	if err != nil {
		t.Fatal(err)
	}
	agg := newTestAggregate(1)
	if err := agg.ApplyAndRecord(&valueSetEvent{V: 7}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, agg); !contextx.IsAfterCommitError(err) {
		t.Fatalf("expected committed publication error: %v", err)
	}
	if len(agg.GetUncommittedEvents()) != 0 || agg.GetExpectedVersion() != 1 {
		t.Fatal("aggregate still has uncommitted changes")
	}
	restored, err := repo.Get(ctx, 1)
	if err != nil || restored.Value != 7 {
		t.Fatalf("durable aggregate: %+v %v", restored, err)
	}
	if err := repo.Save(ctx, agg); err != nil {
		t.Fatal(err)
	}
	if publisher.calls != 1 {
		t.Fatalf("republished committed events %d times", publisher.calls)
	}
}

type committedFailureRepo struct{ flakyRepo }

func (r *committedFailureRepo) Save(context.Context, *retryTestAggregate) error {
	r.saveCalls++
	return contextx.WrapAfterCommitError(errors.NewCode(errors.Concurrency, "snapshot conflict"))
}
func TestCommandDoesNotRetryCommittedSaveEvenWithCustomClassifier(t *testing.T) {
	repo := &committedFailureRepo{}
	svc, err := NewEventSourcedService[*retryTestAggregate, int64](repo, &EventSourcedServiceOptions[*retryTestAggregate, int64]{ConcurrencyRetry: &RetryConfig{MaxRetries: 3}, IsConcurrencyError: func(error) bool { return true }})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	if err := svc.RegisterCommandHandler(&retryTestCommand{}, func(context.Context, IEventSourcedCommand[int64], *retryTestAggregate) error { calls++; return nil }); err != nil {
		t.Fatal(err)
	}
	err = svc.ExecuteCommand(context.Background(), &retryTestCommand{id: 1})
	if !contextx.IsAfterCommitError(err) || calls != 1 || repo.saveCalls != 1 {
		t.Fatalf("repeated committed command: calls=%d saves=%d err=%v", calls, repo.saveCalls, err)
	}
	var exhausted *RetryExhaustedError
	if errors.As(err, &exhausted) {
		t.Fatal("committed error incorrectly classified as exhausted retry")
	}
}
