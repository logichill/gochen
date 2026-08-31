package store

import (
	"gochen/eventing/internal/testutil"
	"testing"
	"time"

	"gochen/testkit/require"

	"gochen/eventing"
)

func TestFilterEventsWithOptionsDoesNotMutateInputOrder(t *testing.T) {
	first := testutil.NewEvent[int64](1, "Agg", "First", 1, nil)
	first.Message.ID = "b"
	first.Message.Timestamp = time.Unix(2, 0)
	second := testutil.NewEvent[int64](1, "Agg", "Second", 2, nil)
	second.Message.ID = "a"
	second.Message.Timestamp = time.Unix(1, 0)
	events := []eventing.Event[int64]{*first, *second}

	result := FilterEventsWithOptions(events, nil)

	require.Len(t, result.Events, 2)
	require.Equal(t, "a", result.Events[0].GetID())
	require.Equal(t, "b", result.Events[1].GetID())
	require.Equal(t, "b", events[0].GetID())
	require.Equal(t, "a", events[1].GetID())
}
