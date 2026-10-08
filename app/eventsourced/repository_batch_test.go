package eventsourced

import (
	"context"
	deventsourced "gochen/domain/eventsourced"
	"gochen/errors"
	"testing"
)

type atomicDomainStoreTest struct {
	mockEventStore
	fail bool
}

func (s *atomicDomainStoreTest) AppendEventsBatch(context.Context, []deventsourced.DomainEventAppend[int64]) error {
	if s.fail {
		return errors.NewCode(errors.Concurrency, "test conflict")
	}
	return nil
}

func TestSaveAllRetainsPendingEventsOnFailure(t *testing.T) {
	storage := &atomicDomainStoreTest{fail: true}
	repo, err := NewAtomicEventSourcedRepository(RepositoryOptions[*TestAggregate, int64]{AggregateType: "TestAggregate", Sample: &TestAggregate{}, Factory: AdaptAggregateFactory(NewTestAggregate), Store: storage, MetadataRegistry: testMetadataRegistry})
	if err != nil {
		t.Fatal(err)
	}
	a, b := NewTestAggregate(1), NewTestAggregate(2)
	for _, agg := range []*TestAggregate{a, b} {
		if err := agg.ApplyAndRecord(&TestEvent{eventType: "TestEvent", data: "pending"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.SaveAll(context.Background(), []*TestAggregate{a, b}); !errors.Is(err, errors.Concurrency) {
		t.Fatalf("expected conflict: %v", err)
	}
	for _, agg := range []*TestAggregate{a, b} {
		if len(agg.GetUncommittedEvents()) != 1 || agg.GetExpectedVersion() != 0 {
			t.Fatal("failed batch committed aggregate state")
		}
	}
	storage.fail = false
	if err := repo.SaveAll(context.Background(), []*TestAggregate{a, b}); err != nil {
		t.Fatal(err)
	}
	for _, agg := range []*TestAggregate{a, b} {
		if len(agg.GetUncommittedEvents()) != 0 || agg.GetExpectedVersion() != 1 {
			t.Fatal("successful batch did not commit aggregate state")
		}
	}
}

func TestAtomicRepositoryRejectsTypedNilStore(t *testing.T) {
	var storage *atomicDomainStoreTest
	opts := RepositoryOptions[*TestAggregate, int64]{AggregateType: "TestAggregate", Sample: &TestAggregate{}, Factory: AdaptAggregateFactory(NewTestAggregate), Store: storage, MetadataRegistry: testMetadataRegistry}
	if repo, err := NewAtomicEventSourcedRepository(opts); repo != nil || !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("accepted typed nil atomic store: %v", err)
	}
	if repo, err := NewEventSourcedRepository(opts); repo != nil || !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("accepted typed nil store: %v", err)
	}
}
