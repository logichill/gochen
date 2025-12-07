package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gochen/eventing"
)

func TestMemoryEventStore_GetEventStreamWithCursor_FilterByAfterAndType(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryEventStore()

	e1 := eventing.NewEvent[int64](1, "Agg", "TypeA", 1, nil)
	e2 := eventing.NewEvent[int64](1, "Agg", "TypeB", 2, nil)
	e3 := eventing.NewEvent[int64](1, "Agg", "TypeA", 3, nil)

	// 统一时间戳，验证同时间戳下的 ID 游标过滤
	now := time.Now()
	e1.Timestamp = now
	e2.Timestamp = now
	e3.Timestamp = now

	require.NoError(t, store.AppendEvents(ctx, 1, []eventing.IStorableEvent[int64]{e1, e2, e3}, 0))

	result, err := store.GetEventStreamWithCursor(ctx, &StreamOptions{
		After: e1.ID,
		Types: []string{"TypeA"},
		Limit: 10,
	})
	require.NoError(t, err)

	require.Len(t, result.Events, 1)
	require.Equal(t, e3.ID, result.Events[0].ID)
	require.False(t, result.HasMore)
	require.Equal(t, e3.ID, result.NextCursor)
}
