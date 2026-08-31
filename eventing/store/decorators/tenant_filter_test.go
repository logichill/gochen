package decorators

import (
	"context"
	"testing"

	"gochen/contextx"
	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/internal/testutil"
	"gochen/eventing/store"
	"gochen/messaging"
	"gochen/testkit/require"
)

type nilMetadataEvent struct {
	*eventing.Event[int64]
}

func (*nilMetadataEvent) GetMetadata() *messaging.Metadata { return nil }

func TestTenantAwareEventStore_LoadEvents_FiltersByTenant(t *testing.T) {
	base := store.NewMemoryEventStore[int64]()
	es := NewTenantAwareEventStore[int64](base)

	ctxA, err := contextx.WithTenantID(context.Background(), "t1")
	require.NoError(t, err)
	ctxB, err := contextx.WithTenantID(context.Background(), "t2")
	require.NoError(t, err)

	e1 := testutil.NewEvent[int64](1, "Agg", "Evt", 1, map[string]any{"x": 1})
	require.NoError(t, contextx.InjectTenantID(ctxA, e1.GetMetadata()))
	require.NoError(t, base.AppendEvents(ctxA, "Agg", 1, []eventing.IStorableEvent[int64]{e1}, 0))

	e2 := testutil.NewEvent[int64](1, "Agg", "Evt", 2, map[string]any{"x": 2})
	require.NoError(t, contextx.InjectTenantID(ctxB, e2.GetMetadata()))
	require.NoError(t, base.AppendEvents(ctxB, "Agg", 1, []eventing.IStorableEvent[int64]{e2}, 1))

	gotA, err := es.LoadEvents(ctxA, "Agg", 1, 0)
	require.NoError(t, err)
	require.Len(t, gotA, 1)
	require.Equal(t, uint64(1), gotA[0].GetVersion())

	gotB, err := es.LoadEvents(ctxB, "Agg", 1, 0)
	require.NoError(t, err)
	require.Len(t, gotB, 1)
	require.Equal(t, uint64(2), gotB[0].GetVersion())
}

func TestTenantAwareEventStore_RequiresTenantForEveryOperation(t *testing.T) {
	base := store.NewMemoryEventStore[int64]()
	es := NewTenantAwareEventStore[int64](base)
	ctx := context.Background()
	evt := testutil.NewEvent[int64](1, "Agg", "Evt", 1, nil)

	require.ErrorIs(t, es.AppendEvents(ctx, "Agg", 1, []eventing.IStorableEvent[int64]{evt}, 0), errors.InvalidInput)
	_, err := es.LoadEvents(ctx, "Agg", 1, 0)
	require.ErrorIs(t, err, errors.InvalidInput)
	_, err = es.StreamEvents(ctx, nil)
	require.ErrorIs(t, err, errors.InvalidInput)
	_, err = es.StreamAggregate(ctx, nil)
	require.ErrorIs(t, err, errors.InvalidInput)
	_, err = es.HasAggregate(ctx, "Agg", 1)
	require.ErrorIs(t, err, errors.InvalidInput)
	_, err = es.GetAggregateVersion(ctx, "Agg", 1)
	require.ErrorIs(t, err, errors.InvalidInput)

	stored, err := base.LoadEvents(ctx, "Agg", 1, 0)
	require.NoError(t, err)
	require.Empty(t, stored)
}

func TestTenantAwareEventStore_RejectsEventWithoutWritableMetadata(t *testing.T) {
	base := store.NewMemoryEventStore[int64]()
	es := NewTenantAwareEventStore[int64](base)
	ctx, err := contextx.WithTenantID(context.Background(), "t1")
	require.NoError(t, err)
	evt := &nilMetadataEvent{Event: testutil.NewEvent[int64](1, "Agg", "Evt", 1, nil)}

	err = es.AppendEvents(ctx, "Agg", 1, []eventing.IStorableEvent[int64]{evt}, 0)
	require.ErrorIs(t, err, errors.InvalidInput)
	stored, loadErr := base.LoadEvents(context.Background(), "Agg", 1, 0)
	require.NoError(t, loadErr)
	require.Empty(t, stored)
}

func TestTenantAwareEventStore_StreamEvents_SkipsEmptyPages(t *testing.T) {
	base := store.NewMemoryEventStore[int64]()
	es := NewTenantAwareEventStore[int64](base)

	ctxA, err := contextx.WithTenantID(context.Background(), "t1")
	require.NoError(t, err)
	ctxB, err := contextx.WithTenantID(context.Background(), "t2")
	require.NoError(t, err)

	// First page: tenant t2 only.
	e1 := testutil.NewEvent[int64](1, "Agg", "Evt", 1, nil)
	require.NoError(t, contextx.InjectTenantID(ctxB, e1.GetMetadata()))
	require.NoError(t, base.AppendEvents(ctxB, "Agg", 1, []eventing.IStorableEvent[int64]{e1}, 0))

	// Second page: tenant t1.
	e2 := testutil.NewEvent[int64](1, "Agg", "Evt", 2, nil)
	require.NoError(t, contextx.InjectTenantID(ctxA, e2.GetMetadata()))
	require.NoError(t, base.AppendEvents(ctxA, "Agg", 1, []eventing.IStorableEvent[int64]{e2}, 1))

	res, err := es.StreamEvents(ctxA, &store.StreamOptions{Limit: 1})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Len(t, res.Events, 1)
	require.Equal(t, uint64(2), res.Events[0].GetVersion())
}

func TestFilterEventCursorsKeepsFilteredEventOrder(t *testing.T) {
	e1 := testutil.NewEvent[int64](1, "Agg", "Created", 1, nil)
	e2 := testutil.NewEvent[int64](1, "Agg", "Updated", 2, nil)
	e3 := testutil.NewEvent[int64](1, "Agg", "Deleted", 3, nil)
	all := []eventing.Event[int64]{*e1, *e2, *e3}
	filtered := []eventing.Event[int64]{*e2, *e3}

	got := filterEventCursors(all, []string{"c1", "c2", "c3"}, filtered)

	require.Equal(t, []string{"c2", "c3"}, got)
}

func TestFilterEventCursorsReturnsNilWhenCursorMissing(t *testing.T) {
	e1 := testutil.NewEvent[int64](1, "Agg", "Created", 1, nil)
	e2 := testutil.NewEvent[int64](1, "Agg", "Updated", 2, nil)

	got := filterEventCursors(
		[]eventing.Event[int64]{*e1, *e2},
		[]string{"c1"},
		[]eventing.Event[int64]{*e1, *e2},
	)

	require.Nil(t, got)
}

func TestTenantAwareEventStore_StreamAggregate_SkipsEmptyPages(t *testing.T) {
	base := store.NewMemoryEventStore[int64]()
	es := NewTenantAwareEventStore[int64](base)

	ctxA, err := contextx.WithTenantID(context.Background(), "t1")
	require.NoError(t, err)
	ctxB, err := contextx.WithTenantID(context.Background(), "t2")
	require.NoError(t, err)

	// First page (afterVersion=0, limit=1): tenant t2 only.
	e1 := testutil.NewEvent[int64](1, "Agg", "Evt", 1, nil)
	require.NoError(t, contextx.InjectTenantID(ctxB, e1.GetMetadata()))
	require.NoError(t, base.AppendEvents(ctxB, "Agg", 1, []eventing.IStorableEvent[int64]{e1}, 0))

	// Second page: tenant t1.
	e2 := testutil.NewEvent[int64](1, "Agg", "Evt", 2, nil)
	require.NoError(t, contextx.InjectTenantID(ctxA, e2.GetMetadata()))
	require.NoError(t, base.AppendEvents(ctxA, "Agg", 1, []eventing.IStorableEvent[int64]{e2}, 1))

	res, err := es.StreamAggregate(ctxA, &store.AggregateStreamOptions[int64]{AggregateType: "Agg", AggregateID: 1, AfterVersion: 0, Limit: 1})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Len(t, res.Events, 1)
	require.Equal(t, uint64(2), res.Events[0].GetVersion())

	// NextVersion should advance to the last version returned by the underlying store.
	require.Equal(t, uint64(2), res.NextVersion)
}
