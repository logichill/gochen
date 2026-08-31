package messaging

import (
	"testing"
	"time"

	"gochen/testkit/require"

	"gochen/clock"
)

func TestNewMessageWithClockUsesInjectedTime(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	message := NewMessageWithClock(clock.NewManualClock(now), "message-1", KindEvent, "Created", nil)
	require.Equal(t, now, message.Timestamp)
}

func TestNewMessageWithClockTreatsTypedNilAsDefaultClock(t *testing.T) {
	var typedNil *clock.ManualClock
	before := time.Now()
	message := NewMessageWithClock(typedNil, "message-1", KindEvent, "Created", nil)
	after := time.Now()
	require.False(t, message.Timestamp.Before(before))
	require.False(t, message.Timestamp.After(after))
}
