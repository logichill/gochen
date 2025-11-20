package cached

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gochen/eventing"
	"gochen/eventing/store"
)

type fakeRecorder struct {
	streamCount int
	streamErr   bool
}

func (f *fakeRecorder) RecordAppend(count int, d time.Duration, err bool) {}
func (f *fakeRecorder) RecordLoad(count int, d time.Duration, err bool)   {}
func (f *fakeRecorder) RecordStream(count int, d time.Duration, err bool) {
	f.streamCount = count
	f.streamErr = err
}

func TestMetricsEventStore_GetEventStreamWithCursor(t *testing.T) {
	ctx := context.Background()
	mem := store.NewMemoryEventStore()
	recorder := &fakeRecorder{}
	ms := NewMetricsEventStore(mem, recorder)

	e1 := eventing.NewEvent(1, "Agg", "TypeA", 1, nil)
	e2 := eventing.NewEvent(1, "Agg", "TypeB", 2, nil)
	now := time.Now()
	e1.Timestamp = now
	e2.Timestamp = now
	require.NoError(t, mem.AppendEvents(ctx, 1, []eventing.IStorableEvent{e1, e2}, 0))

	res, err := ms.GetEventStreamWithCursor(ctx, &store.StreamOptions{
		After: e1.ID,
		Types: []string{"TypeB"},
	})
	require.NoError(t, err)
	require.Len(t, res.Events, 1)
	require.Equal(t, e2.ID, res.Events[0].ID)
	require.False(t, recorder.streamErr)
	require.Equal(t, 1, recorder.streamCount)
}
