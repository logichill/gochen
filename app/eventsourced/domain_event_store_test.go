package eventsourced

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"gochen/testkit/require"

	"gochen/domain"
	deventsourced "gochen/domain/eventsourced"
	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/bus"
	"gochen/eventing/store"
	"gochen/eventing/store/snapshot"
	"gochen/messaging"
	"gochen/messaging/transport/memory"
)

// 复用 repository_test.go 中的 testAggregate/newTestAggregate 定义。

type countingEventStreamStore struct {
	inner                       store.IEventStreamStore[int64]
	loadEventsByTypeCalls       int
	getAggregateVersionByType   int
	getAggregateVersionTypeArgs []string
	streamAggregateCalls        []store.AggregateStreamOptions[int64]
}

func newCountingEventStreamStore(inner store.IEventStreamStore[int64]) *countingEventStreamStore {
	return &countingEventStreamStore{inner: inner}
}

func (s *countingEventStreamStore) AppendEvents(ctx context.Context, aggregateType string, aggregateID int64, events []eventing.IStorableEvent[int64], expectedVersion uint64) error {
	return s.inner.AppendEvents(ctx, aggregateType, aggregateID, events, expectedVersion)
}

func (s *countingEventStreamStore) LoadEvents(ctx context.Context, aggregateType string, aggregateID int64, afterVersion uint64) ([]eventing.Event[int64], error) {
	s.loadEventsByTypeCalls++
	return s.inner.LoadEvents(ctx, aggregateType, aggregateID, afterVersion)
}

func (s *countingEventStreamStore) HasAggregate(ctx context.Context, aggregateType string, aggregateID int64) (bool, error) {
	return s.inner.HasAggregate(ctx, aggregateType, aggregateID)
}

func (s *countingEventStreamStore) GetAggregateVersion(ctx context.Context, aggregateType string, aggregateID int64) (uint64, error) {
	s.getAggregateVersionByType++
	s.getAggregateVersionTypeArgs = append(s.getAggregateVersionTypeArgs, aggregateType)
	return s.inner.GetAggregateVersion(ctx, aggregateType, aggregateID)
}

func (s *countingEventStreamStore) StreamEvents(ctx context.Context, opts *store.StreamOptions) (*store.StreamResult[int64], error) {
	return s.inner.StreamEvents(ctx, opts)
}

func (s *countingEventStreamStore) StreamAggregate(ctx context.Context, opts *store.AggregateStreamOptions[int64]) (*store.AggregateStreamResult[int64], error) {
	if opts != nil {
		s.streamAggregateCalls = append(s.streamAggregateCalls, *opts)
	}
	return s.inner.StreamAggregate(ctx, opts)
}

type stallingAggregateStreamStore struct {
	calls   []store.AggregateStreamOptions[int64]
	batched bool
}

func (s *stallingAggregateStreamStore) AppendEvents(context.Context, string, int64, []eventing.IStorableEvent[int64], uint64) error {
	return nil
}

func (s *stallingAggregateStreamStore) LoadEvents(context.Context, string, int64, uint64) ([]eventing.Event[int64], error) {
	return nil, nil
}

func (s *stallingAggregateStreamStore) HasAggregate(context.Context, string, int64) (bool, error) {
	return true, nil
}

func (s *stallingAggregateStreamStore) GetAggregateVersion(context.Context, string, int64) (uint64, error) {
	return 1, nil
}

func (s *stallingAggregateStreamStore) StreamEvents(context.Context, *store.StreamOptions) (*store.StreamResult[int64], error) {
	return nil, nil
}

func (s *stallingAggregateStreamStore) StreamAggregate(_ context.Context, opts *store.AggregateStreamOptions[int64]) (*store.AggregateStreamResult[int64], error) {
	if opts != nil {
		s.calls = append(s.calls, *opts)
	}
	if s.batched {
		evt := newTestEvent[int64](13, "TestAggregate", "ValueSet", 1, &valueSetEvent{V: 1})
		return &store.AggregateStreamResult[int64]{
			Events:      []eventing.Event[int64]{*evt},
			NextVersion: 1,
			HasMore:     true,
		}, nil
	}
	s.batched = true
	first := newTestEvent[int64](13, "TestAggregate", "ValueSet", 1, &valueSetEvent{V: 1})
	second := newTestEvent[int64](13, "TestAggregate", "ValueSet", 2, &valueSetEvent{V: 2})
	return &store.AggregateStreamResult[int64]{
		Events:      []eventing.Event[int64]{*first, *second},
		NextVersion: 2,
		HasMore:     true,
	}, nil
}

type endlessProgressingAggregateStreamStore struct {
	calls []store.AggregateStreamOptions[int64]
}

func (s *endlessProgressingAggregateStreamStore) AppendEvents(context.Context, string, int64, []eventing.IStorableEvent[int64], uint64) error {
	return nil
}

func (s *endlessProgressingAggregateStreamStore) LoadEvents(context.Context, string, int64, uint64) ([]eventing.Event[int64], error) {
	return nil, nil
}

func (s *endlessProgressingAggregateStreamStore) HasAggregate(context.Context, string, int64) (bool, error) {
	return true, nil
}

func (s *endlessProgressingAggregateStreamStore) GetAggregateVersion(context.Context, string, int64) (uint64, error) {
	return 1, nil
}

func (s *endlessProgressingAggregateStreamStore) StreamEvents(context.Context, *store.StreamOptions) (*store.StreamResult[int64], error) {
	return nil, nil
}

func (s *endlessProgressingAggregateStreamStore) StreamAggregate(_ context.Context, opts *store.AggregateStreamOptions[int64]) (*store.AggregateStreamResult[int64], error) {
	if opts != nil {
		s.calls = append(s.calls, *opts)
	}
	version := uint64(1)
	if opts != nil {
		version = opts.AfterVersion + 1
	}
	evt := newTestEvent[int64](13, "TestAggregate", "ValueSet", version, &valueSetEvent{V: int(version)})
	return &store.AggregateStreamResult[int64]{
		Events:      []eventing.Event[int64]{*evt},
		NextVersion: version,
		HasMore:     true,
	}, nil
}

type notFoundAggregateStreamStore struct {
	calls []store.AggregateStreamOptions[int64]
}

func (s *notFoundAggregateStreamStore) AppendEvents(context.Context, string, int64, []eventing.IStorableEvent[int64], uint64) error {
	return nil
}

func (s *notFoundAggregateStreamStore) LoadEvents(context.Context, string, int64, uint64) ([]eventing.Event[int64], error) {
	return nil, nil
}

func (s *notFoundAggregateStreamStore) HasAggregate(context.Context, string, int64) (bool, error) {
	return false, nil
}

func (s *notFoundAggregateStreamStore) GetAggregateVersion(context.Context, string, int64) (uint64, error) {
	return 0, nil
}

func (s *notFoundAggregateStreamStore) StreamEvents(context.Context, *store.StreamOptions) (*store.StreamResult[int64], error) {
	return nil, nil
}

func (s *notFoundAggregateStreamStore) StreamAggregate(_ context.Context, opts *store.AggregateStreamOptions[int64]) (*store.AggregateStreamResult[int64], error) {
	if opts != nil {
		s.calls = append(s.calls, *opts)
	}
	return nil, errors.NewCode(errors.NotFound, "aggregate stream not found")
}

type typedNilEventIDGenerator struct{}

func (*typedNilEventIDGenerator) Next() (string, error) {
	panic("typed-nil event ID generator must not be called")
}

type typedNilDomainEvent struct {
	eventType string
}

func (e *typedNilDomainEvent) EventType() string { return e.eventType }

// TestNewDomainEventStore_InvalidOptions 验证 NewDomainEventStore InvalidOptions。
func TestNewDomainEventStore_InvalidOptions(t *testing.T) {
	t.Parallel()

	eventStore := store.NewMemoryEventStore[int64]()
	reg, upgraders := newTestRegistryAndUpgraders()

	// 缺少 AggregateType
	_, err := NewDomainEventStore(DomainEventStoreOptions[*testAggregate, int64]{
		AggregateType:    "",
		EventIDGenerator: testEventIDGenerator(),
		EventStore:       eventStore,
		EventRegistry:    reg,
		UpgraderRegistry: upgraders,
	})
	require.Error(t, err)

	// 缺少 EventStore
	_, err = NewDomainEventStore(DomainEventStoreOptions[*testAggregate, int64]{
		AggregateType:    "TestAggregate",
		EventIDGenerator: testEventIDGenerator(),
		EventStore:       nil,
		EventRegistry:    reg,
		UpgraderRegistry: upgraders,
	})
	require.Error(t, err)

	// typed-nil generator 必须在构造期被拒绝，不能延迟到 AppendEvents 时 panic。
	var eventIDGenerator *typedNilEventIDGenerator
	storeAdapter, err := NewDomainEventStore(DomainEventStoreOptions[*testAggregate, int64]{
		AggregateType:    "TestAggregate",
		EventIDGenerator: eventIDGenerator,
		EventStore:       eventStore,
		EventRegistry:    reg,
		UpgraderRegistry: upgraders,
	})
	require.Nil(t, storeAdapter)
	require.True(t, errors.Is(err, errors.InvalidInput))
}

func TestNewDomainEventStore_RequiresEventIDGenerator(t *testing.T) {
	t.Parallel()

	eventStore := store.NewMemoryEventStore[int64]()
	reg, upgraders := newTestRegistryAndUpgraders()

	storeAdapter, err := NewDomainEventStore(DomainEventStoreOptions[*testAggregate, int64]{
		AggregateType:    "TestAggregate",
		EventStore:       eventStore,
		EventRegistry:    reg,
		UpgraderRegistry: upgraders,
	})
	require.Nil(t, storeAdapter)
	require.True(t, errors.Is(err, errors.InvalidInput))
}

// TestNewDomainEventStore_PublishEventsWithoutOutbox_AllowsDirectPublish 验证 NewDomainEventStore PublishEventsWithoutOutbox AllowsDirectPublish。
func TestNewDomainEventStore_PublishEventsWithoutOutbox_AllowsDirectPublish(t *testing.T) {
	t.Parallel()

	eventStore := store.NewMemoryEventStore[int64]()
	reg, upgraders := newTestRegistryAndUpgraders()
	require.NoError(t, reg.Register("ValueSet", func() any { return &valueSetEvent{} }))

	transport := memory.NewMemoryTransport(10, 1)
	require.NoError(t, transport.Start(context.Background()))
	t.Cleanup(func() {
		_ = transport.Stop(context.Background())
	})

	messageBus := messaging.NewMessageBus(transport)
	eventBus := bus.NewEventBus(messageBus)

	handlerCalled := make(chan struct{}, 1)
	unsub, err := eventBus.SubscribeEvent(context.Background(), "*", bus.EventHandlerFunc(func(ctx context.Context, evt eventing.IEvent) error {
		handlerCalled <- struct{}{}
		return nil
	}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = unsub(context.Background()) })

	storeAdapter, err := NewDomainEventStore(DomainEventStoreOptions[*testAggregate, int64]{
		AggregateType:    "TestAggregate",
		EventIDGenerator: testEventIDGenerator(),
		EventStore:       eventStore,
		EventBus:         eventBus,
		PublishEvents:    true,
		OutboxRepo:       nil,
		EventRegistry:    reg,
		UpgraderRegistry: upgraders,
	})
	require.NoError(t, err)

	agg := newTestAggregate(1)
	require.NoError(t, agg.ApplyAndRecord(&valueSetEvent{V: 42}))
	require.NoError(t, storeAdapter.AppendEvents(context.Background(), agg.GetID(), agg.GetUncommittedEvents(), 0))

	select {
	case <-handlerCalled:
	case <-time.After(2 * time.Second):
		t.Fatalf("handler was not called")
	}
}

// TestDomainEventStore_ImplementsIEventStore 验证 DomainEventStore ImplementsIEventStore。
func TestDomainEventStore_ImplementsIEventStore(t *testing.T) {
	t.Parallel()

	eventStore := store.NewMemoryEventStore[int64]()
	reg, upgraders := newTestRegistryAndUpgraders()

	storeAdapter, err := NewDomainEventStore(DomainEventStoreOptions[*testAggregate, int64]{
		AggregateType:    "TestAggregate",
		EventIDGenerator: testEventIDGenerator(),
		EventStore:       eventStore,
		EventRegistry:    reg,
		UpgraderRegistry: upgraders,
	})
	require.NoError(t, err)

	var _ deventsourced.IDomainEventStore[int64] = storeAdapter
}

func TestDomainEventStore_AppendEvents_SetsSchemaVersionFromRegistry(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	eventStore := store.NewMemoryEventStore[int64]()
	reg, upgraders := newTestRegistryAndUpgraders()
	require.NoError(t, reg.RegisterWithVersion("ValueSet", 3, func() any { return &valueSetEvent{} }))

	storeAdapter, err := NewDomainEventStore(DomainEventStoreOptions[*testAggregate, int64]{
		AggregateType:    "TestAggregate",
		EventIDGenerator: testEventIDGenerator(),
		EventStore:       eventStore,
		EventRegistry:    reg,
		UpgraderRegistry: upgraders,
	})
	require.NoError(t, err)

	agg := newTestAggregate(1)
	require.NoError(t, agg.ApplyAndRecord(&valueSetEvent{V: 42}))
	require.NoError(t, storeAdapter.AppendEvents(ctx, agg.GetID(), agg.GetUncommittedEvents(), 0))

	loaded, err := eventStore.LoadEvents(ctx, "TestAggregate", agg.GetID(), 0)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	require.Equal(t, 3, loaded[0].EventSchemaVersion())
}

func TestDomainEventStore_AppendEvents_DoesNotPersistWhenEventIDGenerationFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	eventStore := store.NewMemoryEventStore[int64]()
	reg, upgraders := newTestRegistryAndUpgraders()
	require.NoError(t, reg.Register("ValueSet", func() any { return &valueSetEvent{} }))

	storeAdapter, err := NewDomainEventStore(DomainEventStoreOptions[*testAggregate, int64]{
		AggregateType: "TestAggregate",
		EventIDGenerator: testStringGeneratorFunc(func() (string, error) {
			return "", errors.New("generator unavailable")
		}),
		EventStore:       eventStore,
		EventRegistry:    reg,
		UpgraderRegistry: upgraders,
	})
	require.NoError(t, err)

	err = storeAdapter.AppendEvents(ctx, 1, []domain.IDomainEvent{&valueSetEvent{V: 42}}, 0)
	require.True(t, errors.Is(err, errors.Dependency))

	exists, err := eventStore.HasAggregate(ctx, "TestAggregate", 1)
	require.NoError(t, err)
	require.False(t, exists)
}

func TestDomainEventStore_AppendEvents_RejectsTypedNilDomainEvent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	eventStore := store.NewMemoryEventStore[int64]()
	reg, upgraders := newTestRegistryAndUpgraders()

	storeAdapter, err := NewDomainEventStore(DomainEventStoreOptions[*testAggregate, int64]{
		AggregateType:    "TestAggregate",
		EventIDGenerator: testEventIDGenerator(),
		EventStore:       eventStore,
		EventRegistry:    reg,
		UpgraderRegistry: upgraders,
	})
	require.NoError(t, err)

	var event *typedNilDomainEvent
	err = storeAdapter.AppendEvents(ctx, 1, []domain.IDomainEvent{event}, 0)
	require.True(t, errors.Is(err, errors.InvalidInput))

	exists, err := eventStore.HasAggregate(ctx, "TestAggregate", 1)
	require.NoError(t, err)
	require.False(t, exists)
}

// 用于验证 RestoreAggregate 在回放阶段对未命中 handler 直接 fail-fast。
type autoAgg struct {
	*deventsourced.EventSourcedAggregate[int64]
	Applied int
}

func newAutoAgg(id int64) *autoAgg {
	a := &autoAgg{}
	agg, err := deventsourced.InitAggregate[int64](testMetadataRegistry, a, id, "AutoAgg")
	if err != nil {
		panic(err)
	}
	a.EventSourcedAggregate = agg
	return a
}

type handledEvent struct{}

func (e *handledEvent) EventType() string { return "Handled" }

type unhandledEvent struct{}

func (e *unhandledEvent) EventType() string { return "Unhandled" }

func (a *autoAgg) WhenHandled(e *handledEvent) { a.Applied++ }

func TestDomainEventStore_RestoreAggregate_FailFastOnMissingHandler(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	eventStore := store.NewMemoryEventStore[int64]()
	reg, upgraders := newTestRegistryAndUpgraders()
	require.NoError(t, reg.Register("Handled", func() any { return &handledEvent{} }))
	require.NoError(t, reg.Register("Unhandled", func() any { return &unhandledEvent{} }))

	storeAdapter, err := NewDomainEventStore(DomainEventStoreOptions[*autoAgg, int64]{
		AggregateType:    "AutoAgg",
		EventIDGenerator: testEventIDGenerator(),
		EventStore:       eventStore,
		EventRegistry:    reg,
		UpgraderRegistry: upgraders,
	})
	require.NoError(t, err)

	require.NoError(t, storeAdapter.AppendEvents(ctx, 1, []domain.IDomainEvent{
		&handledEvent{},
		&unhandledEvent{},
	}, 0))

	// 回放遇到未命中 handler 的事件应 fail-fast
	_, err = storeAdapter.RestoreAggregate(ctx, newAutoAgg(1))
	require.Error(t, err)
}

func TestDomainEventStore_RestoreAggregate_SnapshotOnlyNotFoundStreamKeepsSnapshotVersion(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	eventStore := &notFoundAggregateStreamStore{}
	snapshotStore := snapshot.NewMemoryStore[int64]()
	snapshotData, err := json.Marshal(&testAggregate{Value: 42})
	require.NoError(t, err)
	require.NoError(t, snapshotStore.SaveSnapshot(ctx, snapshot.Snapshot[int64]{
		AggregateID:   11,
		AggregateType: "TestAggregate",
		Version:       7,
		Data:          snapshotData,
		Timestamp:     time.Now(),
	}))
	snapshotMgr := snapshot.NewManager[int64](snapshotStore, snapshot.DefaultConfig())
	reg, upgraders := newTestRegistryAndUpgraders()
	require.NoError(t, reg.Register("ValueSet", func() any { return &valueSetEvent{} }))

	storeAdapter, err := NewDomainEventStore(DomainEventStoreOptions[*testAggregate, int64]{
		AggregateType:    "TestAggregate",
		EventIDGenerator: testEventIDGenerator(),
		EventStore:       eventStore,
		SnapshotManager:  snapshotMgr,
		EventRegistry:    reg,
		UpgraderRegistry: upgraders,
	})
	require.NoError(t, err)

	agg := newTestAggregate(11)
	result, err := storeAdapter.RestoreAggregate(ctx, agg)
	require.NoError(t, err)
	require.True(t, result.FromSnapshot)
	require.Equal(t, uint64(7), result.SnapshotVersion)
	require.Equal(t, uint64(7), result.Version)
	require.True(t, result.Exists)
	require.Equal(t, 42, agg.Value)
	require.Equal(t, uint64(7), agg.GetVersion())
	require.Len(t, eventStore.calls, 1)
	require.Equal(t, uint64(7), eventStore.calls[0].AfterVersion)
}

func TestDomainEventStore_RestoreAggregate_StopsStalledStream(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	eventStore := &stallingAggregateStreamStore{}
	reg, upgraders := newTestRegistryAndUpgraders()
	require.NoError(t, reg.Register("ValueSet", func() any { return &valueSetEvent{} }))

	storeAdapter, err := NewDomainEventStore(DomainEventStoreOptions[*testAggregate, int64]{
		AggregateType:    "TestAggregate",
		EventIDGenerator: testEventIDGenerator(),
		EventStore:       eventStore,
		EventRegistry:    reg,
		UpgraderRegistry: upgraders,
	})
	require.NoError(t, err)

	_, err = storeAdapter.RestoreAggregate(ctx, newTestAggregate(13))
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Internal))
	require.Len(t, eventStore.calls, 2)
	require.Equal(t, uint64(0), eventStore.calls[0].AfterVersion)
	require.Equal(t, uint64(2), eventStore.calls[1].AfterVersion)
}

func TestDomainEventStore_RestoreAggregate_StopsAfterMaxBatchRounds(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	eventStore := &endlessProgressingAggregateStreamStore{}
	reg, upgraders := newTestRegistryAndUpgraders()
	require.NoError(t, reg.Register("ValueSet", func() any { return &valueSetEvent{} }))

	storeAdapter, err := NewDomainEventStore(DomainEventStoreOptions[*testAggregate, int64]{
		AggregateType:    "TestAggregate",
		EventIDGenerator: testEventIDGenerator(),
		EventStore:       eventStore,
		EventRegistry:    reg,
		UpgraderRegistry: upgraders,
	})
	require.NoError(t, err)

	_, err = storeAdapter.RestoreAggregate(ctx, newTestAggregate(13))
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Internal))
	require.Len(t, eventStore.calls, restoreAggregateBatchMaxRounds)
	require.Equal(t, uint64(0), eventStore.calls[0].AfterVersion)
	require.Equal(t, uint64(restoreAggregateBatchMaxRounds-1), eventStore.calls[len(eventStore.calls)-1].AfterVersion)
}

func TestDomainEventStore_ExistsAndVersion_AreScopedByAggregateType(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memoryStore := store.NewMemoryEventStore[int64]()
	eventStore := newCountingEventStreamStore(memoryStore)
	reg, upgraders := newTestRegistryAndUpgraders()
	require.NoError(t, reg.Register("ValueSet", func() any { return &valueSetEvent{} }))

	storeAdapter, err := NewDomainEventStore(DomainEventStoreOptions[*testAggregate, int64]{
		AggregateType:    "TypeA",
		EventIDGenerator: testEventIDGenerator(),
		EventStore:       eventStore,
		EventRegistry:    reg,
		UpgraderRegistry: upgraders,
	})
	require.NoError(t, err)

	require.NoError(t, memoryStore.AppendEvents(ctx, "TypeB", 8, []eventing.IStorableEvent[int64]{
		newTestEvent[int64](8, "TypeB", "ValueSet", 1, &valueSetEvent{V: 99}),
	}, 0))

	exists, err := storeAdapter.Exists(ctx, 7)
	require.NoError(t, err)
	require.False(t, exists)

	version, err := storeAdapter.GetAggregateVersion(ctx, 7)
	require.NoError(t, err)
	require.Zero(t, version)

	require.NoError(t, memoryStore.AppendEvents(ctx, "TypeA", 7, []eventing.IStorableEvent[int64]{
		newTestEvent[int64](7, "TypeA", "ValueSet", 1, &valueSetEvent{V: 42}),
	}, 0))

	exists, err = storeAdapter.Exists(ctx, 7)
	require.NoError(t, err)
	require.True(t, exists)

	version, err = storeAdapter.GetAggregateVersion(ctx, 7)
	require.NoError(t, err)
	require.Equal(t, uint64(1), version)
	require.Zero(t, eventStore.loadEventsByTypeCalls)
	require.NotEmpty(t, eventStore.streamAggregateCalls)
}

func TestDomainEventStore_ExistsUsesBoundedAggregateStream(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memoryStore := store.NewMemoryEventStore[int64]()
	eventStore := newCountingEventStreamStore(memoryStore)
	reg, upgraders := newTestRegistryAndUpgraders()
	require.NoError(t, reg.Register("ValueSet", func() any { return &valueSetEvent{} }))

	storeAdapter, err := NewDomainEventStore(DomainEventStoreOptions[*testAggregate, int64]{
		AggregateType:    "TestAggregate",
		EventIDGenerator: testEventIDGenerator(),
		EventStore:       eventStore,
		EventRegistry:    reg,
		UpgraderRegistry: upgraders,
	})
	require.NoError(t, err)

	require.NoError(t, memoryStore.AppendEvents(ctx, "TestAggregate", 9, []eventing.IStorableEvent[int64]{
		newTestEvent[int64](9, "TestAggregate", "ValueSet", 1, &valueSetEvent{V: 1}),
		newTestEvent[int64](9, "TestAggregate", "ValueSet", 2, &valueSetEvent{V: 2}),
	}, 0))

	exists, err := storeAdapter.Exists(ctx, 9)
	require.NoError(t, err)
	require.True(t, exists)
	require.Zero(t, eventStore.loadEventsByTypeCalls)
	require.Len(t, eventStore.streamAggregateCalls, 1)
	require.Equal(t, 1, eventStore.streamAggregateCalls[0].Limit)
	require.Equal(t, "TestAggregate", eventStore.streamAggregateCalls[0].AggregateType)
	require.Equal(t, int64(9), eventStore.streamAggregateCalls[0].AggregateID)
}

func TestDomainEventStore_GetAggregateVersionUsesTypedStoreQuery(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memoryStore := store.NewMemoryEventStore[int64]()
	eventStore := newCountingEventStreamStore(memoryStore)
	reg, upgraders := newTestRegistryAndUpgraders()
	require.NoError(t, reg.Register("ValueSet", func() any { return &valueSetEvent{} }))

	storeAdapter, err := NewDomainEventStore(DomainEventStoreOptions[*testAggregate, int64]{
		AggregateType:    "TestAggregate",
		EventIDGenerator: testEventIDGenerator(),
		EventStore:       eventStore,
		EventRegistry:    reg,
		UpgraderRegistry: upgraders,
	})
	require.NoError(t, err)

	events := make([]eventing.IStorableEvent[int64], 0, 1001)
	for i := 1; i <= 1001; i++ {
		version := uint64(i)
		events = append(events, newTestEvent[int64](11, "TestAggregate", "ValueSet", version, &valueSetEvent{V: i}))
	}
	require.NoError(t, memoryStore.AppendEvents(ctx, "TestAggregate", 11, events, 0))

	version, err := storeAdapter.GetAggregateVersion(ctx, 11)
	require.NoError(t, err)
	require.Equal(t, uint64(1001), version)
	require.Zero(t, eventStore.loadEventsByTypeCalls)
	require.Empty(t, eventStore.streamAggregateCalls)
	require.Equal(t, 1, eventStore.getAggregateVersionByType)
	require.Equal(t, []string{"TestAggregate"}, eventStore.getAggregateVersionTypeArgs)
}

func TestDomainEventStore_GetAggregateVersionUsesTypedStoreQueryWhenStreamWouldStall(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	eventStore := &stallingAggregateStreamStore{}
	reg, upgraders := newTestRegistryAndUpgraders()
	require.NoError(t, reg.Register("ValueSet", func() any { return &valueSetEvent{} }))

	storeAdapter, err := NewDomainEventStore(DomainEventStoreOptions[*testAggregate, int64]{
		AggregateType:    "TestAggregate",
		EventIDGenerator: testEventIDGenerator(),
		EventStore:       eventStore,
		EventRegistry:    reg,
		UpgraderRegistry: upgraders,
	})
	require.NoError(t, err)

	version, err := storeAdapter.GetAggregateVersion(ctx, 13)
	require.NoError(t, err)
	require.Equal(t, uint64(1), version)
	require.Empty(t, eventStore.calls)
}
