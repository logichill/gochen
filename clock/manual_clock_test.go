package clock

import (
	"testing"
	"time"
)

func TestManualClock_NowAndAdvance(t *testing.T) {
	start := time.Unix(123, 0).UTC()
	c := NewManualClock(start)

	if got := c.Now(); !got.Equal(start) {
		t.Fatalf("Now() = %v, want %v", got, start)
	}

	c.Advance(10 * time.Second)
	want := start.Add(10 * time.Second)
	if got := c.Now(); !got.Equal(want) {
		t.Fatalf("Now() after Advance = %v, want %v", got, want)
	}
}

func TestManualClock_Timer_FiresOnAdvance(t *testing.T) {
	c := NewManualClock(time.Unix(0, 0).UTC())
	timer := c.NewTimer(10 * time.Millisecond)

	c.Advance(9 * time.Millisecond)
	select {
	case <-timer.C():
		t.Fatalf("timer fired early")
	default:
	}

	c.Advance(1 * time.Millisecond)
	select {
	case <-timer.C():
		// ok
	default:
		t.Fatalf("expected timer to fire")
	}
}

func TestManualClock_Timer_ResetToPastFiresImmediately(t *testing.T) {
	c := NewManualClock(time.Unix(0, 0).UTC())
	timer := c.NewTimer(1 * time.Hour)

	// Move time forward first.
	c.Advance(10 * time.Second)

	// Reset to a deadline that is already <= now.
	_ = timer.Reset(-1 * time.Second)

	select {
	case <-timer.C():
		// ok
	default:
		t.Fatalf("expected timer to fire immediately on Reset to past")
	}
}

func TestManualClock_Ticker_TicksOnAdvance(t *testing.T) {
	c := NewManualClock(time.Unix(0, 0).UTC())
	tk, err := c.NewTicker(10 * time.Millisecond)
	if err != nil {
		t.Fatalf("NewTicker() error = %v", err)
	}
	defer tk.Stop()

	c.Advance(35 * time.Millisecond)

	// Expect at least 3 ticks (10ms, 20ms, 30ms). Delivery is best-effort,
	// but with a 1-buffer and immediate draining, this should be stable.
	got := 0
drain:
	for {
		select {
		case <-tk.C():
			got++
		default:
			break drain
		}
	}
	if got < 3 {
		t.Fatalf("ticks = %d, want >= 3", got)
	}
}

func TestManualClock_NewTicker_InvalidIntervalReturnsError(t *testing.T) {
	c := NewManualClock(time.Unix(0, 0).UTC())

	tk, err := c.NewTicker(0)
	if err == nil {
		t.Fatalf("NewTicker() error = nil, want non-nil")
	}
	if tk != nil {
		t.Fatalf("NewTicker() ticker = %v, want nil", tk)
	}
}
