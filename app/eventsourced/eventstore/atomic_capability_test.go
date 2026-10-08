package eventstore_test

import (
	"context"
	"testing"

	esapp "gochen/app/eventsourced"
	esstore "gochen/app/eventsourced/eventstore"
	"gochen/domain"
	deventsourced "gochen/domain/eventsourced"
	"gochen/errors"
	"gochen/eventing/store/snapshot"
)

// 包装器刻意只声明单项端口，用于验证底层能力不能被假冒。
type singleStreamStore struct {
	esstore.IAggregateEventStore[int64]
}

func TestDomainStoreAndHistoryUseAggregateOnlyBackend(t *testing.T) {
	opts := modeOptions(t)
	opts.PublishMode = esstore.PublishModePersistOnly
	opts.EventStore = singleStreamStore{opts.EventStore}
	adapter, err := esstore.NewDomainEventStore(opts)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := adapter.AppendEvents(ctx, 1, []domain.IDomainEvent{&valueSetEvent{V: 42}}, 0); err != nil {
		t.Fatal(err)
	}
	aggregate := newTestAggregate(1)
	result, err := adapter.RestoreAggregate(ctx, aggregate)
	if err != nil {
		t.Fatal(err)
	}
	if aggregate.Value != 42 || result.Version != 1 || !result.Exists {
		t.Fatalf("restore = %+v, value=%d", result, aggregate.Value)
	}
	page, err := esstore.LoadEventHistoryPage(ctx, opts.EventStore, "TestAggregate", 1, 1, 20, nil)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Entries) != 1 {
		t.Fatalf("history = %+v", page)
	}
}

func TestAtomicCapabilitiesAreSelectedDuringAssembly(t *testing.T) {
	opts := modeOptions(t)
	opts.PublishMode = esstore.PublishModePersistOnly
	single, err := esstore.NewDomainEventStore(opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := single.(deventsourced.IAtomicDomainEventStore[int64]); ok {
		t.Fatal("single store advertises atomic capability")
	}
	repoOpts := esapp.RepositoryOptions[*testAggregate, int64]{AggregateType: "TestAggregate", Sample: &testAggregate{}, Factory: esapp.AdaptAggregateFactory(newTestAggregate), Store: single, MetadataRegistry: testMetadataRegistry}
	ordinary, err := esapp.NewEventSourcedRepository(repoOpts)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := any(ordinary).(deventsourced.IAtomicEventSourcedRepository[*testAggregate, int64]); ok {
		t.Fatal("single repository advertises atomic capability")
	}
	if r, err := esapp.NewAtomicEventSourcedRepository(repoOpts); r != nil || !errors.Is(err, errors.Unsupported) {
		t.Fatalf("atomic repository accepted single store: %v", err)
	}

	atomic, err := esstore.NewAtomicDomainEventStore(opts)
	if err != nil {
		t.Fatal(err)
	}
	repoOpts.Store = atomic
	repo, err := esapp.NewAtomicEventSourcedRepository(repoOpts)
	if err != nil {
		t.Fatal(err)
	}
	var _ deventsourced.IAtomicEventSourcedRepository[*testAggregate, int64] = repo

	disabled, err := esstore.NewSnapshottingRepository("TestAggregate", repo, esstore.SnapshottingRepositoryOptions[int64]{})
	if err != nil || disabled != repo {
		t.Fatalf("disabled decorator changed capability: %v", err)
	}
	manager := snapshot.NewManager[int64](snapshot.NewMemoryStore[int64](), snapshot.DefaultConfig())
	decorated, err := esstore.NewSnapshottingRepository("TestAggregate", repo, esstore.SnapshottingRepositoryOptions[int64]{SnapshotManager: manager})
	if decorated != nil || !errors.Is(err, errors.Unsupported) {
		t.Fatalf("active snapshot decorator silently hid atomic capability: %v", err)
	}
}

func TestAtomicStoreRejectsNonAtomicAndTypedNilDependencies(t *testing.T) {
	opts := modeOptions(t)
	opts.PublishMode = esstore.PublishModePersistOnly
	opts.EventStore = singleStreamStore{opts.EventStore}
	if v, err := esstore.NewAtomicDomainEventStore(opts); v != nil || !errors.Is(err, errors.Unsupported) {
		t.Fatalf("accepted single stream store: %v", err)
	}
	var nilStore *modeEventStore
	opts.EventStore = nilStore
	if v, err := esstore.NewAtomicDomainEventStore(opts); v != nil || !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("accepted typed nil store: %v", err)
	}
	var nilRepository *esapp.EventSourcedRepository[*testAggregate, int64]
	if v, err := esstore.NewSnapshottingRepository("TestAggregate", nilRepository, esstore.SnapshottingRepositoryOptions[int64]{}); v != nil || !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("accepted typed nil inner: %v", err)
	}
}
