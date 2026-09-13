package decorator

import (
	"context"
	"gochen/eventing/internal/testutil"
	"testing"

	"gochen/testkit/require"

	"gochen/contextx"
	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/store"
)

func TestContextAwareEventStore_GetAggregateVersionFiltersByTenant(t *testing.T) {
	base := store.NewMemoryEventStore[int64]()
	es := NewContextAwareEventStore[int64](base)

	ctxA, err := contextx.WithTenantID(context.Background(), "tenant-a")
	require.NoError(t, err)
	ctxB, err := contextx.WithTenantID(context.Background(), "tenant-b")
	require.NoError(t, err)

	e1 := testutil.NewEvent[int64](1, "Order", "Created", 1, nil)
	require.NoError(t, contextx.InjectTenantID(ctxA, e1.GetMetadata()))
	e2 := testutil.NewEvent[int64](1, "Order", "Updated", 2, nil)
	require.NoError(t, contextx.InjectTenantID(ctxB, e2.GetMetadata()))
	require.NoError(t, base.AppendEvents(context.Background(), "Order", 1, []eventing.IStorableEvent[int64]{e1, e2}, 0))

	versionA, err := es.GetAggregateVersion(ctxA, "Order", 1)
	require.NoError(t, err)
	require.Equal(t, uint64(1), versionA)

	versionB, err := es.GetAggregateVersion(ctxB, "Order", 1)
	require.NoError(t, err)
	require.Equal(t, uint64(2), versionB)
}

func TestContextAwareEventStore_GetAggregateVersionUsesVersionQueryOrStream(t *testing.T) {
	base := store.NewMemoryEventStore[int64]()
	probe := &versionProbeStore{IEventStreamStore: base}
	es := NewContextAwareEventStore[int64](probe)

	e1 := testutil.NewEvent[int64](1, "Order", "Created", 1, nil)
	e2 := testutil.NewEvent[int64](1, "Order", "Updated", 2, nil)
	require.NoError(t, base.AppendEvents(context.Background(), "Order", 1, []eventing.IStorableEvent[int64]{e1, e2}, 0))
	ctx, err := contextx.WithTenantID(context.Background(), "tenant-a")
	require.NoError(t, err)
	e3 := testutil.NewEvent[int64](1, "Order", "UpdatedAgain", 3, nil)
	require.NoError(t, contextx.InjectTenantID(ctx, e3.GetMetadata()))
	require.NoError(t, base.AppendEvents(context.Background(), "Order", 1, []eventing.IStorableEvent[int64]{e3}, 2))

	version, err := es.GetAggregateVersion(context.Background(), "Order", 1)
	require.NoError(t, err)
	require.Equal(t, uint64(3), version)
	require.Equal(t, 1, probe.getVersionByTypeCalls)
	require.Zero(t, probe.loadEventsByTypeCalls)
	require.Zero(t, probe.streamAggregateCalls)

	version, err = es.GetAggregateVersion(ctx, "Order", 1)
	require.NoError(t, err)
	require.Equal(t, uint64(3), version)
	require.Zero(t, probe.loadEventsByTypeCalls)
	require.NotZero(t, probe.streamAggregateCalls)
}

func TestTenantAwareEventStore_GetAggregateVersionFiltersByTenant(t *testing.T) {
	base := store.NewMemoryEventStore[int64]()
	es := NewTenantAwareEventStore[int64](base)

	ctxA, err := contextx.WithTenantID(context.Background(), "tenant-a")
	require.NoError(t, err)
	ctxB, err := contextx.WithTenantID(context.Background(), "tenant-b")
	require.NoError(t, err)

	e1 := testutil.NewEvent[int64](1, "Order", "Created", 1, nil)
	require.NoError(t, contextx.InjectTenantID(ctxA, e1.GetMetadata()))
	e2 := testutil.NewEvent[int64](1, "Order", "Updated", 2, nil)
	require.NoError(t, contextx.InjectTenantID(ctxB, e2.GetMetadata()))
	require.NoError(t, base.AppendEvents(context.Background(), "Order", 1, []eventing.IStorableEvent[int64]{e1, e2}, 0))

	version, err := es.GetAggregateVersion(ctxA, "Order", 1)
	require.NoError(t, err)
	require.Equal(t, uint64(1), version)
}

func TestTracingEventStore_GetAggregateVersionDelegates(t *testing.T) {
	base := store.NewMemoryEventStore[int64]()
	es := NewTracingEventStore[int64](base)

	e1 := testutil.NewEvent[int64](1, "Order", "Created", 1, nil)
	e2 := testutil.NewEvent[int64](1, "Order", "Updated", 2, nil)
	require.NoError(t, base.AppendEvents(context.Background(), "Order", 1, []eventing.IStorableEvent[int64]{e1, e2}, 0))

	version, err := es.GetAggregateVersion(context.Background(), "Order", 1)
	require.NoError(t, err)
	require.Equal(t, uint64(2), version)
}

func TestDecoratedEventStore_GetAggregateVersionRequiresAggregateType(t *testing.T) {
	base := store.NewMemoryEventStore[int64]()
	stores := []store.IEventStreamStore[int64]{
		NewContextAwareEventStore[int64](base),
		NewTenantAwareEventStore[int64](base),
		NewTracingEventStore[int64](base),
	}

	for _, es := range stores {
		for _, aggregateType := range []string{"", "   "} {
			_, err := es.GetAggregateVersion(context.Background(), aggregateType, 1)
			require.Error(t, err)
			require.Truef(t, errors.Is(err, errors.InvalidInput), "expected invalid input, got %v", err)
		}
	}
}

func TestDecoratedEventStore_AppendEventsRejectsTypedNilEvent(t *testing.T) {
	base := store.NewMemoryEventStore[int64]()
	stores := []store.IEventStreamStore[int64]{
		NewContextAwareEventStore[int64](base),
		NewTenantAwareEventStore[int64](base),
		NewTracingEventStore[int64](base),
	}
	var typedNil *eventing.Event[int64]

	for _, eventStore := range stores {
		err := eventStore.AppendEvents(context.Background(), "Order", 1, []eventing.IStorableEvent[int64]{typedNil}, 0)
		require.Error(t, err)
		require.Truef(t, errors.Is(err, errors.InvalidInput), "expected invalid input, got %v", err)
	}
}

type versionProbeStore struct {
	store.IEventStreamStore[int64]

	loadEventsByTypeCalls int
	streamAggregateCalls  int
	getVersionByTypeCalls int
}

func (s *versionProbeStore) LoadEvents(ctx context.Context, aggregateType string, aggregateID int64, afterVersion uint64) ([]eventing.Event[int64], error) {
	s.loadEventsByTypeCalls++
	return s.IEventStreamStore.LoadEvents(ctx, aggregateType, aggregateID, afterVersion)
}

func (s *versionProbeStore) StreamAggregate(ctx context.Context, opts *store.AggregateStreamOptions[int64]) (*store.AggregateStreamResult[int64], error) {
	s.streamAggregateCalls++
	return s.IEventStreamStore.StreamAggregate(ctx, opts)
}

func (s *versionProbeStore) GetAggregateVersion(ctx context.Context, aggregateType string, aggregateID int64) (uint64, error) {
	s.getVersionByTypeCalls++
	return s.IEventStreamStore.GetAggregateVersion(ctx, aggregateType, aggregateID)
}
