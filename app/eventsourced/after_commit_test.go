package eventsourced

import (
	"context"
	esstore "gochen/app/eventsourced/eventstore"
	"testing"

	"gochen/auth"
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

func TestApplicationReturnsCommittedAggregateWhenPublicationFails(t *testing.T) {
	ctx := t.Context()
	events := store.NewMemoryEventStore[int64]()
	reg, upgraders := newTestRegistryAndUpgraders()
	if err := reg.Register("ValueSet", func() any { return &valueSetEvent{} }); err != nil {
		t.Fatal(err)
	}
	publisher := &failingPublishBus{}
	adapter, err := esstore.NewDomainEventStore(esstore.DomainEventStoreOptions[int64]{
		PublishMode: esstore.PublishModeDirect, AggregateType: "TestAggregate", EventIDGenerator: testEventIDGenerator(), EventStore: events, EventBus: publisher, EventRegistry: reg, UpgraderRegistry: upgraders,
	})
	if err != nil {
		t.Fatal(err)
	}
	repo, err := newTestEventSourcedRepository[*testAggregate, int64]("TestAggregate", &testAggregate{}, AdaptAggregateFactory(newTestAggregate), adapter)
	if err != nil {
		t.Fatal(err)
	}
	app, err := NewApplication[*testAggregate, int64](repo, Options[*testAggregate, int64]{
		Resource: "test.aggregate", Authorizer: auth.Allow(),
		ConcurrencyRetry: &RetryConfig{MaxRetries: 3},
	})
	if err != nil {
		t.Fatal(err)
	}
	created, err := app.ExecuteNew(ctx, ActionCreate, func(context.Context) (*testAggregate, error) {
		aggregate := newTestAggregate(1)
		return aggregate, aggregate.ApplyAndRecord(&valueSetEvent{V: 5})
	})
	if !contextx.IsAfterCommitError(err) || created == nil || created.GetExpectedVersion() != 1 || len(created.GetUncommittedEvents()) != 0 {
		t.Fatalf("create lost committed result: %+v, %v", created, err)
	}
	calls := 0
	updated, err := app.Execute(ctx, ActionUpdate, 1, func(_ context.Context, aggregate *testAggregate) error {
		calls++
		return aggregate.ApplyAndRecord(&valueSetEvent{V: 42})
	})
	if !contextx.IsAfterCommitError(err) || updated == nil || updated.Value != 42 || updated.GetExpectedVersion() != 2 {
		t.Fatalf("update lost committed result: %+v, %v", updated, err)
	}
	if calls != 1 || publisher.calls != 2 {
		t.Fatalf("committed command was retried: commands=%d publications=%d", calls, publisher.calls)
	}
	stored, err := repo.Get(ctx, 1)
	if err != nil || stored.Value != 42 || stored.GetVersion() != 2 {
		t.Fatalf("persisted state: %+v, %v", stored, err)
	}
}
