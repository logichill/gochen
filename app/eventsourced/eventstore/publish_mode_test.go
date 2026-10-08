package eventstore_test

import (
	"context"
	"testing"

	esstore "gochen/app/eventsourced/eventstore"
	"gochen/contextx"
	"gochen/domain"
	deventsourced "gochen/domain/eventsourced"
	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/bus"
	"gochen/eventing/outbox"
	"gochen/eventing/store"
)

type modeEventBus struct {
	bus.IEventBus
	calls int
	err   error
}

func (b *modeEventBus) PublishEvents(context.Context, []eventing.IEvent) error {
	b.calls++
	return b.err
}

type modeOutbox struct {
	outbox.IOutboxRepository[int64]
	calls int
	err   error
}

func (o *modeOutbox) SaveWithEvents(context.Context, int64, []eventing.Event[int64]) error {
	o.calls++
	return o.err
}

type modeEventStore struct {
	*store.MemoryEventStore[int64]
	calls int
	err   error
}

func (s *modeEventStore) AppendEvents(ctx context.Context, typ string, id int64, events []eventing.IStorableEvent[int64], expected uint64) error {
	s.calls++
	if s.err != nil {
		return s.err
	}
	return s.MemoryEventStore.AppendEvents(ctx, typ, id, events, expected)
}

func modeOptions(t *testing.T) esstore.DomainEventStoreOptions[int64] {
	t.Helper()
	reg, upgraders := newTestRegistryAndUpgraders()
	if err := reg.Register("ValueSet", func() any { return &valueSetEvent{} }); err != nil {
		t.Fatal(err)
	}
	return esstore.DomainEventStoreOptions[int64]{
		AggregateType: "TestAggregate", EventIDGenerator: testEventIDGenerator(),
		EventStore:    &modeEventStore{MemoryEventStore: store.NewMemoryEventStore[int64]()},
		EventRegistry: reg, UpgraderRegistry: upgraders,
	}
}

func TestPublishModeRejectsMissingAndConflictingDependencies(t *testing.T) {
	var nilBus *modeEventBus
	var nilOutbox *modeOutbox
	var nilStore *modeEventStore
	cases := []struct {
		name   string
		mode   esstore.PublishMode
		bus    bus.IEventBus
		outbox outbox.IOutboxRepository[int64]
	}{
		{name: "missing mode"},
		{name: "unknown mode", mode: "automatic"},
		{name: "direct missing bus", mode: esstore.PublishModeDirect},
		{name: "direct typed nil bus", mode: esstore.PublishModeDirect, bus: nilBus},
		{name: "direct with outbox", mode: esstore.PublishModeDirect, bus: &modeEventBus{}, outbox: &modeOutbox{}},
		{name: "direct with typed nil outbox", mode: esstore.PublishModeDirect, bus: &modeEventBus{}, outbox: nilOutbox},
		{name: "outbox missing repository", mode: esstore.PublishModeOutbox},
		{name: "outbox typed nil repository", mode: esstore.PublishModeOutbox, outbox: nilOutbox},
		{name: "outbox with bus", mode: esstore.PublishModeOutbox, bus: &modeEventBus{}, outbox: &modeOutbox{}},
		{name: "outbox with typed nil bus", mode: esstore.PublishModeOutbox, bus: nilBus, outbox: &modeOutbox{}},
		{name: "persist with bus", mode: esstore.PublishModePersistOnly, bus: &modeEventBus{}},
		{name: "persist with outbox", mode: esstore.PublishModePersistOnly, outbox: &modeOutbox{}},
		{name: "persist with typed nil bus", mode: esstore.PublishModePersistOnly, bus: nilBus},
		{name: "persist with typed nil outbox", mode: esstore.PublishModePersistOnly, outbox: nilOutbox},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := modeOptions(t)
			opts.PublishMode = tc.mode
			opts.EventBus = tc.bus
			opts.OutboxRepo = tc.outbox
			adapter, err := esstore.NewDomainEventStore(opts)
			if adapter != nil || !errors.Is(err, errors.InvalidInput) {
				t.Fatalf("expected invalid configuration, got %v, %v", adapter, err)
			}
			if opts.EventStore.(*modeEventStore).calls != 0 {
				t.Fatal("invalid configuration performed a write")
			}
		})
	}
	opts := modeOptions(t)
	opts.PublishMode = esstore.PublishModePersistOnly
	opts.EventStore = nilStore
	if _, err := esstore.NewDomainEventStore(opts); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("typed nil event store: %v", err)
	}
}

func TestPublishModeRoutesWritesAndPreservesFailureSemantics(t *testing.T) {
	failure := errors.NewCode(errors.Dependency, "injected failure")
	cases := []struct {
		name                        string
		mode                        esstore.PublishMode
		storeErr, busErr, outboxErr error
		stores, publishes, outboxes int
		committed                   bool
	}{
		{name: "persist only", mode: esstore.PublishModePersistOnly, stores: 1},
		{name: "direct", mode: esstore.PublishModeDirect, stores: 1, publishes: 1},
		{name: "outbox", mode: esstore.PublishModeOutbox, outboxes: 1},
		{name: "append fails before publish", mode: esstore.PublishModeDirect, storeErr: failure, stores: 1},
		{name: "publish fails after append", mode: esstore.PublishModeDirect, busErr: failure, stores: 1, publishes: 1, committed: true},
		{name: "outbox fails", mode: esstore.PublishModeOutbox, outboxErr: failure, outboxes: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := modeOptions(t)
			opts.PublishMode = tc.mode
			stream := opts.EventStore.(*modeEventStore)
			stream.err = tc.storeErr
			publisher := &modeEventBus{err: tc.busErr}
			ob := &modeOutbox{err: tc.outboxErr}
			if tc.mode == esstore.PublishModeDirect {
				opts.EventBus = publisher
			}
			if tc.mode == esstore.PublishModeOutbox {
				opts.OutboxRepo = ob
			}
			adapter, err := esstore.NewDomainEventStore(opts)
			if err != nil {
				t.Fatal(err)
			}
			err = adapter.AppendEvents(context.Background(), 1, []domain.IDomainEvent{&valueSetEvent{V: 7}}, 0)
			wantErr := tc.storeErr != nil || tc.busErr != nil || tc.outboxErr != nil
			if (err != nil) != wantErr || contextx.IsAfterCommitError(err) != tc.committed {
				t.Fatalf("unexpected commit/error state: %v", err)
			}
			if wantErr && !errors.Is(err, failure) {
				t.Fatalf("lost underlying error: %v", err)
			}
			if stream.calls != tc.stores || publisher.calls != tc.publishes || ob.calls != tc.outboxes {
				t.Fatalf("wrong path: store=%d publish=%d outbox=%d", stream.calls, publisher.calls, ob.calls)
			}
			version, err := stream.GetAggregateVersion(context.Background(), "TestAggregate", 1)
			if tc.stores == 1 && tc.storeErr == nil && (err != nil || version != 1) {
				t.Fatalf("append was not durable: version=%d err=%v", version, err)
			}
		})
	}
}

func TestPublishModeBatchRequiresPersistenceOnly(t *testing.T) {
	for _, mode := range []esstore.PublishMode{esstore.PublishModePersistOnly, esstore.PublishModeDirect, esstore.PublishModeOutbox} {
		t.Run(string(mode), func(t *testing.T) {
			opts := modeOptions(t)
			opts.PublishMode = mode
			publisher := &modeEventBus{}
			ob := &modeOutbox{}
			if mode == esstore.PublishModeDirect {
				opts.EventBus = publisher
			}
			if mode == esstore.PublishModeOutbox {
				opts.OutboxRepo = ob
			}
			adapter, err := esstore.NewAtomicDomainEventStore(opts)
			if mode == esstore.PublishModePersistOnly {
				if err != nil {
					t.Fatal(err)
				}
				err = adapter.AppendEventsBatch(context.Background(), []deventsourced.DomainEventAppend[int64]{{AggregateID: 1, Events: []domain.IDomainEvent{&valueSetEvent{V: 9}}}})
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, errors.Unsupported) || adapter != nil {
				t.Fatalf("expected rejection during construction: %v", err)
			}

			version, queryErr := opts.EventStore.GetAggregateVersion(context.Background(), "TestAggregate", 1)
			exists := version > 0
			if queryErr != nil || exists != (mode == esstore.PublishModePersistOnly) {
				t.Fatalf("unexpected batch persistence: %v %v", exists, queryErr)
			}
			if publisher.calls != 0 || ob.calls != 0 {
				t.Fatal("batch must not publish or enqueue outbox records")
			}
		})
	}
}
