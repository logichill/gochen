package decorators

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"gochen/contextx"
	"gochen/eventing"
	"gochen/eventing/store"
)

func TestTenantAwareEventStore_LoadEvents_FiltersByTenant(t *testing.T) {
	base := store.NewMemoryEventStore()
	es := NewTenantAwareEventStore[int64](base)

	ctxA, err := contextx.WithTenantID(context.Background(), "t1")
	require.NoError(t, err)
	ctxB, err := contextx.WithTenantID(context.Background(), "t2")
	require.NoError(t, err)

	e1 := eventing.NewEvent[int64](1, "Agg", "Evt", 1, map[string]any{"x": 1})
	require.NoError(t, contextx.InjectTenantID(ctxA, e1.GetMetadata()))
	require.NoError(t, base.AppendEvents(ctxA, 1, []eventing.IStorableEvent[int64]{e1}, 0))

	e2 := eventing.NewEvent[int64](1, "Agg", "Evt", 2, map[string]any{"x": 2})
	require.NoError(t, contextx.InjectTenantID(ctxB, e2.GetMetadata()))
	require.NoError(t, base.AppendEvents(ctxB, 1, []eventing.IStorableEvent[int64]{e2}, 1))

	gotA, err := es.LoadEvents(ctxA, 1, 0)
	require.NoError(t, err)
	require.Len(t, gotA, 1)
	require.Equal(t, uint64(1), gotA[0].GetVersion())

	gotB, err := es.LoadEvents(ctxB, 1, 0)
	require.NoError(t, err)
	require.Len(t, gotB, 1)
	require.Equal(t, uint64(2), gotB[0].GetVersion())
}

func TestTenantAwareEventStore_StreamEvents_SkipsEmptyPages(t *testing.T) {
	base := store.NewMemoryEventStore()
	es := NewTenantAwareEventStore[int64](base)

	ctxA, err := contextx.WithTenantID(context.Background(), "t1")
	require.NoError(t, err)
	ctxB, err := contextx.WithTenantID(context.Background(), "t2")
	require.NoError(t, err)

	// First page: tenant t2 only.
	e1 := eventing.NewEvent[int64](1, "Agg", "Evt", 1, nil)
	require.NoError(t, contextx.InjectTenantID(ctxB, e1.GetMetadata()))
	require.NoError(t, base.AppendEvents(ctxB, 1, []eventing.IStorableEvent[int64]{e1}, 0))

	// Second page: tenant t1.
	e2 := eventing.NewEvent[int64](1, "Agg", "Evt", 2, nil)
	require.NoError(t, contextx.InjectTenantID(ctxA, e2.GetMetadata()))
	require.NoError(t, base.AppendEvents(ctxA, 1, []eventing.IStorableEvent[int64]{e2}, 1))

	res, err := es.StreamEvents(ctxA, &store.StreamOptions{Limit: 1})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Len(t, res.Events, 1)
	require.Equal(t, uint64(2), res.Events[0].GetVersion())
}

func TestTenantAwareEventStore_StreamAggregate_SkipsEmptyPages(t *testing.T) {
	base := store.NewMemoryEventStore()
	es := NewTenantAwareEventStore[int64](base)

	ctxA, err := contextx.WithTenantID(context.Background(), "t1")
	require.NoError(t, err)
	ctxB, err := contextx.WithTenantID(context.Background(), "t2")
	require.NoError(t, err)

	// First page (afterVersion=0, limit=1): tenant t2 only.
	e1 := eventing.NewEvent[int64](1, "Agg", "Evt", 1, nil)
	require.NoError(t, contextx.InjectTenantID(ctxB, e1.GetMetadata()))
	require.NoError(t, base.AppendEvents(ctxB, 1, []eventing.IStorableEvent[int64]{e1}, 0))

	// Second page: tenant t1.
	e2 := eventing.NewEvent[int64](1, "Agg", "Evt", 2, nil)
	require.NoError(t, contextx.InjectTenantID(ctxA, e2.GetMetadata()))
	require.NoError(t, base.AppendEvents(ctxA, 1, []eventing.IStorableEvent[int64]{e2}, 1))

	res, err := es.StreamAggregate(ctxA, &store.AggregateStreamOptions[int64]{AggregateType: "Agg", AggregateID: 1, AfterVersion: 0, Limit: 1})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Len(t, res.Events, 1)
	require.Equal(t, uint64(2), res.Events[0].GetVersion())

	// NextVersion should advance to the last version returned by the underlying store.
	require.Equal(t, uint64(2), res.NextVersion)
}
