package eventsourced

import (
	"context"
	"testing"

	esstore "gochen/app/eventsourced/eventstore"

	"gochen/eventing/store"
	"gochen/testkit/require"
)

// TestEventSourcedRepository_SaveAndLoad 验证 EventSourcedRepository SaveAndLoad。
func TestEventSourcedRepository_SaveAndLoad(t *testing.T) {
	ctx := context.Background()
	eventStore := store.NewMemoryEventStore[int64]()
	reg, upgraders := newTestRegistryAndUpgraders()
	require.NoError(t, reg.Register("ValueSet", func() any { return &valueSetEvent{} }))

	adapter, err := esstore.NewDomainEventStore(esstore.DomainEventStoreOptions[int64]{
		PublishMode:      esstore.PublishModePersistOnly,
		AggregateType:    "TestAggregate",
		EventIDGenerator: testEventIDGenerator(),
		EventStore:       eventStore,
		EventRegistry:    reg,
		UpgraderRegistry: upgraders,
	})
	require.NoError(t, err)

	repo, err := newTestEventSourcedRepository[*testAggregate]("TestAggregate", &testAggregate{}, AdaptAggregateFactory(newTestAggregate), adapter)
	require.NoError(t, err)
	require.NotNil(t, repo)

	agg := newTestAggregate(1)
	require.NoError(t, agg.ApplyAndRecord(&valueSetEvent{V: 42}))

	require.NoError(t, repo.Save(ctx, agg))

	loaded, err := repo.Get(ctx, 1)
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
