package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"gochen/messaging"
	"gochen/messaging/deadletter"
	"gochen/testkit/require"
)

// TestSink_WriteAndEntriesSnapshot 验证内存 DLQ sink 的写入与快照语义。
func TestSink_WriteAndEntriesSnapshot(t *testing.T) {
	sink := NewSink()
	now := time.Now()
	entry := deadletter.Entry{
		Message:     &messaging.Message{ID: "m1", Type: "test"},
		HandlerType: "handler",
		Err:         errors.New("boom"),
		OccurredAt:  now,
	}

	require.NoError(t, sink.Write(context.Background(), entry))

	entries := sink.Entries()
	require.Len(t, entries, 1)
	require.Equal(t, "m1", entries[0].Message.GetID())
	require.Equal(t, "test", entries[0].Message.GetType())
	require.Equal(t, "handler", entries[0].HandlerType)
	require.Error(t, entries[0].Err)
	require.True(t, entries[0].OccurredAt.Equal(now))

	entries[0].HandlerType = "mutated"

	again := sink.Entries()
	require.Len(t, again, 1)
	require.Equal(t, "handler", again[0].HandlerType)
}
