package eventsourced

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	deventsourced "gochen/domain/eventsourced"
	"gochen/eventing/bus"
	"gochen/eventing/store"
	"gochen/messaging"
	memorytransport "gochen/messaging/transport/memory"
)

// 复用 repository_test.go 中的 testAggregate/newTestAggregate 定义。

func TestNewDomainEventStore_InvalidOptions(t *testing.T) {
	t.Parallel()

	eventStore := store.NewMemoryEventStore()

	// 缺少 AggregateType
	_, err := NewDomainEventStore(DomainEventStoreOptions[*testAggregate, int64]{
		AggregateType: "",
		Factory:       newTestAggregate,
		EventStore:    eventStore,
	})
	require.Error(t, err)

	// 缺少 Factory
	_, err = NewDomainEventStore(DomainEventStoreOptions[*testAggregate, int64]{
		AggregateType: "TestAggregate",
		Factory:       nil,
		EventStore:    eventStore,
	})
	require.Error(t, err)

	// 缺少 EventStore
	_, err = NewDomainEventStore(DomainEventStoreOptions[*testAggregate, int64]{
		AggregateType: "TestAggregate",
		Factory:       newTestAggregate,
		EventStore:    nil,
	})
	require.Error(t, err)
}

func TestNewDomainEventStore_PublishEventsRequiresOutbox(t *testing.T) {
	t.Parallel()

	eventStore := store.NewMemoryEventStore()

	transport := memorytransport.NewMemoryTransport(10, 1)
	require.NoError(t, transport.Start(context.Background()))
	t.Cleanup(func() {
		_ = transport.Close()
	})

	messageBus := messaging.NewMessageBus(transport)
	eventBus := bus.NewEventBus(messageBus)

	_, err := NewDomainEventStore(DomainEventStoreOptions[*testAggregate, int64]{
		AggregateType: "TestAggregate",
		Factory:       newTestAggregate,
		EventStore:    eventStore,
		EventBus:      eventBus,
		PublishEvents: true,
		OutboxRepo:    nil,
	})
	require.Error(t, err)
}

// 确认 DomainEventStore 满足领域层 IEventStore 接口约束，避免泛型签名变更时静默失配。
func TestDomainEventStore_ImplementsIEventStore(t *testing.T) {
	t.Parallel()

	eventStore := store.NewMemoryEventStore()

	storeAdapter, err := NewDomainEventStore(DomainEventStoreOptions[*testAggregate, int64]{
		AggregateType: "TestAggregate",
		Factory:       newTestAggregate,
		EventStore:    eventStore,
	})
	require.NoError(t, err)

	var _ deventsourced.IEventStore[int64] = storeAdapter
}
