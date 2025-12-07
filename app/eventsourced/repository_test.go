package eventsourced

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"gochen/domain"
	deventsourced "gochen/domain/eventsourced"
	"gochen/eventing/store"
)

// 测试用领域事件
type valueSetEvent struct{ V int }

func (e *valueSetEvent) EventType() string { return "ValueSet" }

// 测试用聚合
type testAggregate struct {
	*deventsourced.EventSourcedAggregate[int64]
	Value int
}

func newTestAggregate(id int64) *testAggregate {
	return &testAggregate{
		EventSourcedAggregate: deventsourced.NewEventSourcedAggregate[int64](id, "TestAggregate"),
	}
}

func (a *testAggregate) ApplyEvent(evt domain.IDomainEvent) error {
	if e, ok := evt.(*valueSetEvent); ok {
		a.Value = e.V
	}
	return a.EventSourcedAggregate.ApplyEvent(evt)
}

func TestEventSourcedRepository_SaveAndLoad(t *testing.T) {
	ctx := context.Background()
	eventStore := store.NewMemoryEventStore()

	adapter, err := NewDomainEventStore(DomainEventStoreOptions[*testAggregate, int64]{
		AggregateType: "TestAggregate",
		Factory:       newTestAggregate,
		EventStore:    eventStore,
	})
	require.NoError(t, err)

	repo, err := deventsourced.NewEventSourcedRepository[*testAggregate]("TestAggregate", newTestAggregate, adapter)
	require.NoError(t, err)
	require.NotNil(t, repo)

	agg := newTestAggregate(1)
	require.NoError(t, agg.ApplyAndRecord(&valueSetEvent{V: 42}))

	require.NoError(t, repo.Save(ctx, agg))

	loaded, err := repo.GetByID(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, 42, loaded.Value)
	require.Equal(t, uint64(1), loaded.GetVersion())

	exists, err := repo.Exists(ctx, 1)
	require.NoError(t, err)
	require.True(t, exists)

	version, err := repo.GetAggregateVersion(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, uint64(1), version)
}
