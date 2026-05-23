package eventsourced

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"gochen/eventing"
	"gochen/eventing/store"
)

// TestGetEventHistoryPage_PaginatesByVersion 验证 EventHistoryPage PaginatesByVersion。
func TestGetEventHistoryPage_PaginatesByVersion(t *testing.T) {
	ctx := context.Background()

	es := store.NewMemoryEventStore()
	for i := 1; i <= 3; i++ {
		evt := eventing.NewEvent[int64](1, "Agg", "E", uint64(i), map[string]any{"i": i})
		evt.SchemaVersion = 1
		require.NoError(t, es.AppendEvents(ctx, 1, []eventing.IStorableEvent[int64]{evt}, uint64(i-1)))
	}

	page1, err := LoadEventHistoryPage(ctx, es, "Agg", 1, 1, 2, nil)
	require.NoError(t, err)
	require.Equal(t, 3, page1.Total)
	require.Len(t, page1.Entries, 2)

	page2, err := LoadEventHistoryPage(ctx, es, "Agg", 1, 2, 2, nil)
	require.NoError(t, err)
	require.Equal(t, 3, page2.Total)
	require.Len(t, page2.Entries, 1)
}

func TestGetEventHistoryPage_TotalIsScopedByAggregateType(t *testing.T) {
	ctx := context.Background()

	es := store.NewMemoryEventStore()
	for i := 1; i <= 2; i++ {
		evt := eventing.NewEvent[int64](1, "AggA", "E", uint64(i), map[string]any{"i": i})
		evt.SchemaVersion = 1
		require.NoError(t, es.AppendEvents(ctx, 1, []eventing.IStorableEvent[int64]{evt}, uint64(i-1)))
	}
	for i := 1; i <= 3; i++ {
		evt := eventing.NewEvent[int64](1, "AggB", "E", uint64(i), map[string]any{"i": i})
		evt.SchemaVersion = 1
		require.NoError(t, es.AppendEvents(ctx, 1, []eventing.IStorableEvent[int64]{evt}, uint64(i-1)))
	}

	page, err := LoadEventHistoryPage(ctx, es, "AggA", 1, 1, 10, nil)
	require.NoError(t, err)
	require.Equal(t, 2, page.Total)
	require.Len(t, page.Entries, 2)
	for _, entry := range page.Entries {
		require.Equal(t, "AggA", entry.AggregateType)
	}
}
