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

func TestManualClockReleasesInactiveTimers(t *testing.T) {
	c := NewManualClock(time.Unix(0, 0))
	for range 100 {
		c.NewTimer(time.Hour).Stop()
		ticker, err := c.NewTicker(time.Second)
		if err != nil {
			t.Fatal(err)
		}
		ticker.Stop()
		c.NewTimer(time.Second)
	}
	c.Advance(time.Second)
	if len(c.timers) != 0 || len(c.tickers) != 0 {
		t.Fatalf("inactive timers retained: timers=%d, tickers=%d", len(c.timers), len(c.tickers))
	}
}

func TestManualTimerResetDiscardsExpiredTick(t *testing.T) {
	c := NewManualClock(time.Unix(0, 0))
	timer := c.NewTimer(time.Second)
	c.Advance(time.Second)
	timer.Reset(time.Hour)
	select {
	case timestamp := <-timer.C():
		t.Fatalf("received stale tick after Reset: %v", timestamp)
	default:
	}
	c.Advance(time.Hour)
	select {
	case timestamp := <-timer.C():
		if !timestamp.Equal(c.Now()) {
			t.Fatalf("reset timer fired at %v, want %v", timestamp, c.Now())
		}
	default:
		t.Fatal("reset timer did not fire")
	}
	timer.Reset(time.Second)
	c.Advance(time.Second)
	if !timer.Stop() {
		t.Fatal("Stop must report the unconsumed timer as active")
	}
	select {
	case <-timer.C():
		t.Fatal("received stale tick after Stop")
	default:
	}
}
