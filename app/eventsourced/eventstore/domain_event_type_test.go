package eventstore_test

import (
	"context"
	"testing"

	esstore "gochen/app/eventsourced/eventstore"
	"gochen/domain"
	deventsourced "gochen/domain/eventsourced"
	"gochen/errors"
	"gochen/eventing/store"
)

type registeredValueEvent struct {
	Amount int `json:"amount"`
}

func (registeredValueEvent) EventType() string { return "RegisteredValue" }

type differentValueEvent registeredValueEvent

func (differentValueEvent) EventType() string { return "RegisteredValue" }

func registeredValueOptions(t *testing.T) esstore.DomainEventStoreOptions[int64] {
	t.Helper()
	opts := modeOptions(t)
	if err := opts.EventRegistry.Register("RegisteredValue", func() any { return &registeredValueEvent{} }); err != nil {
		t.Fatal(err)
	}
	return opts
}

func TestDomainEventStoreRejectsMismatchedTypesBeforeAnySideEffect(t *testing.T) {
	var typedNil *registeredValueEvent
	cases := []struct {
		name   string
		events []domain.IDomainEvent
		code   errors.ErrorCode
	}{
		{name: "different value", events: []domain.IDomainEvent{differentValueEvent{Amount: 1}}, code: errors.InvalidInput},
		{name: "different pointer", events: []domain.IDomainEvent{&differentValueEvent{Amount: 1}}, code: errors.InvalidInput},
		{name: "later mismatch", events: []domain.IDomainEvent{registeredValueEvent{Amount: 1}, differentValueEvent{Amount: 2}}, code: errors.InvalidInput},
		{name: "nil", events: []domain.IDomainEvent{nil}, code: errors.InvalidInput},
		{name: "typed nil", events: []domain.IDomainEvent{typedNil}, code: errors.InvalidInput},
		{name: "unknown", events: []domain.IDomainEvent{unknownBalanceEvent{}}, code: errors.NotFound},
	}
	for _, mode := range []esstore.PublishMode{esstore.PublishModePersistOnly, esstore.PublishModeDirect, esstore.PublishModeOutbox} {
		for _, tc := range cases {
			t.Run(string(mode)+"/"+tc.name, func(t *testing.T) {
				opts := registeredValueOptions(t)
				opts.PublishMode = mode
				publisher, ob := &modeEventBus{}, &modeOutbox{}
				if mode == esstore.PublishModeDirect {
					opts.EventBus = publisher
				}
				if mode == esstore.PublishModeOutbox {
					opts.OutboxRepo = ob
				}
				adapter, err := esstore.NewDomainEventStore(opts)
				if err != nil {
					t.Fatal(err)
				}
				err = adapter.AppendEvents(t.Context(), 1, tc.events, 0)
				if !errors.Is(err, tc.code) {
					t.Fatalf("want %s, got %v", tc.code, err)
				}
				persisted := opts.EventStore.(*modeEventStore)
				if persisted.calls != 0 || publisher.calls != 0 || ob.calls != 0 {
					t.Fatalf("invalid events caused side effects: append=%d publish=%d outbox=%d", persisted.calls, publisher.calls, ob.calls)
				}
				version, err := persisted.GetAggregateVersion(t.Context(), "TestAggregate", 1)
				if err != nil || version != 0 {
					t.Fatalf("invalid events changed version: %d, %v", version, err)
				}
			})
		}
	}
}

type typeCheckedAtomicStore struct {
	*modeEventStore
	batchCalls int
}

func (s *typeCheckedAtomicStore) AppendEventsBatch(ctx context.Context, batch []store.EventAppend[int64]) error {
	s.batchCalls++
	return s.MemoryEventStore.AppendEventsBatch(ctx, batch)
}

func TestAtomicDomainEventStoreValidatesAllEventTypesBeforeAppend(t *testing.T) {
	opts := registeredValueOptions(t)
	opts.PublishMode = esstore.PublishModePersistOnly
	persisted := &typeCheckedAtomicStore{modeEventStore: opts.EventStore.(*modeEventStore)}
	opts.EventStore = persisted
	adapter, err := esstore.NewAtomicDomainEventStore(opts)
	if err != nil {
		t.Fatal(err)
	}
	batch := []deventsourced.DomainEventAppend[int64]{
		{AggregateID: 1, Events: []domain.IDomainEvent{registeredValueEvent{Amount: 1}}},
		{AggregateID: 2, Events: []domain.IDomainEvent{registeredValueEvent{Amount: 2}, differentValueEvent{Amount: 3}}},
	}
	if err := adapter.AppendEventsBatch(t.Context(), batch); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("want type mismatch, got %v", err)
	}
	if persisted.calls != 0 || persisted.batchCalls != 0 {
		t.Fatalf("invalid batch reached persistence: append=%d batch=%d", persisted.calls, persisted.batchCalls)
	}
	for _, id := range []int64{1, 2} {
		version, err := persisted.GetAggregateVersion(t.Context(), "TestAggregate", id)
		if err != nil || version != 0 {
			t.Fatalf("invalid batch changed aggregate %d: version=%d err=%v", id, version, err)
		}
	}
	batch[1].Events[1] = &registeredValueEvent{Amount: 3}
	if err := adapter.AppendEventsBatch(t.Context(), batch); err != nil {
		t.Fatal(err)
	}
	if persisted.batchCalls != 1 {
		t.Fatalf("valid pointer/value batch was not appended once: %d", persisted.batchCalls)
	}
}
