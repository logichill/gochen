package eventsourced

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gochen/domain"
	deventsourced "gochen/domain/eventsourced"
	"gochen/eventing"
	"gochen/eventing/bus"
	"gochen/eventing/store"
	"gochen/messaging"
	memorytransport "gochen/messaging/transport/memory"
)

// 复用 repository_test.go 中的 testAggregate/newTestAggregate 定义。

// TestNewDomainEventStore_InvalidOptions 验证 NewDomainEventStore InvalidOptions。
func TestNewDomainEventStore_InvalidOptions(t *testing.T) {
	t.Parallel()

	eventStore := store.NewMemoryEventStore()
	reg, upgraders := newTestRegistryAndUpgraders()

	// 缺少 AggregateType
	_, err := NewDomainEventStore(DomainEventStoreOptions[*testAggregate, int64]{
		AggregateType:    "",
		EventStore:       eventStore,
		EventRegistry:    reg,
		UpgraderRegistry: upgraders,
	})
	require.Error(t, err)

	// 缺少 EventStore
	_, err = NewDomainEventStore(DomainEventStoreOptions[*testAggregate, int64]{
		AggregateType:    "TestAggregate",
		EventStore:       nil,
		EventRegistry:    reg,
		UpgraderRegistry: upgraders,
	})
	require.Error(t, err)
}

// TestNewDomainEventStore_PublishEventsWithoutOutbox_AllowsDirectPublish 验证 NewDomainEventStore PublishEventsWithoutOutbox AllowsDirectPublish。
func TestNewDomainEventStore_PublishEventsWithoutOutbox_AllowsDirectPublish(t *testing.T) {
	t.Parallel()

	eventStore := store.NewMemoryEventStore()
	reg, upgraders := newTestRegistryAndUpgraders()
	require.NoError(t, reg.Register("ValueSet", func() any { return &valueSetEvent{} }))

	transport := memorytransport.NewMemoryTransport(10, 1)
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

	eventStore := store.NewMemoryEventStore()
	reg, upgraders := newTestRegistryAndUpgraders()

	storeAdapter, err := NewDomainEventStore(DomainEventStoreOptions[*testAggregate, int64]{
		AggregateType:    "TestAggregate",
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
	eventStore := store.NewMemoryEventStore()
	reg, upgraders := newTestRegistryAndUpgraders()
	require.NoError(t, reg.RegisterWithVersion("ValueSet", 3, func() any { return &valueSetEvent{} }))

	storeAdapter, err := NewDomainEventStore(DomainEventStoreOptions[*testAggregate, int64]{
		AggregateType:    "TestAggregate",
		EventStore:       eventStore,
		EventRegistry:    reg,
		UpgraderRegistry: upgraders,
	})
	require.NoError(t, err)

	agg := newTestAggregate(1)
	require.NoError(t, agg.ApplyAndRecord(&valueSetEvent{V: 42}))
	require.NoError(t, storeAdapter.AppendEvents(ctx, agg.GetID(), agg.GetUncommittedEvents(), 0))

	loaded, err := eventStore.LoadEvents(ctx, agg.GetID(), 0)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	require.Equal(t, 3, loaded[0].EventSchemaVersion())
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
	eventStore := store.NewMemoryEventStore()
	reg, upgraders := newTestRegistryAndUpgraders()
	require.NoError(t, reg.Register("Handled", func() any { return &handledEvent{} }))
	require.NoError(t, reg.Register("Unhandled", func() any { return &unhandledEvent{} }))

	storeAdapter, err := NewDomainEventStore(DomainEventStoreOptions[*autoAgg, int64]{
		AggregateType:    "AutoAgg",
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

func TestDomainEventStore_ExistsAndVersion_AreScopedByAggregateType(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	eventStore := store.NewMemoryEventStore()
	reg, upgraders := newTestRegistryAndUpgraders()
	require.NoError(t, reg.Register("ValueSet", func() any { return &valueSetEvent{} }))

	storeAdapter, err := NewDomainEventStore(DomainEventStoreOptions[*testAggregate, int64]{
		AggregateType:    "TypeA",
		EventStore:       eventStore,
		EventRegistry:    reg,
		UpgraderRegistry: upgraders,
	})
	require.NoError(t, err)

	require.NoError(t, eventStore.AppendEvents(ctx, 7, []eventing.IStorableEvent[int64]{
		eventing.NewEvent[int64](7, "TypeB", "ValueSet", 1, &valueSetEvent{V: 99}),
	}, 0))

	exists, err := storeAdapter.Exists(ctx, 7)
	require.NoError(t, err)
	require.False(t, exists)

	version, err := storeAdapter.GetAggregateVersion(ctx, 7)
	require.NoError(t, err)
	require.Zero(t, version)

	require.NoError(t, eventStore.AppendEvents(ctx, 7, []eventing.IStorableEvent[int64]{
		eventing.NewEvent[int64](7, "TypeA", "ValueSet", 1, &valueSetEvent{V: 42}),
	}, 0))

	exists, err = storeAdapter.Exists(ctx, 7)
	require.NoError(t, err)
	require.True(t, exists)

	version, err = storeAdapter.GetAggregateVersion(ctx, 7)
	require.NoError(t, err)
	require.Equal(t, uint64(1), version)
}
