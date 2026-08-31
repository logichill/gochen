package clock

import (
	"testing"
	"time"

	"gochen/errors"
)

func TestRealClock_Now(t *testing.T) {
	c := NewRealClock()
	before := time.Now()
	now := c.Now()
	after := time.Now()

	if now.Before(before) || now.After(after) {
		t.Fatalf("Now() = %v, want between %v and %v", now, before, after)
	}
}

func TestRealClock_NewTimer(t *testing.T) {
	c := NewRealClock()
	timer := c.NewTimer(10 * time.Millisecond)
	select {
	case <-timer.C():
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timer did not fire in time")
	}

	timer.Reset(100 * time.Millisecond)
	timer.Stop()
}

func TestRealClock_NewTicker(t *testing.T) {
	c := NewRealClock()

	// 正常 ticker
	ticker, err := c.NewTicker(10 * time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	select {
	case <-ticker.C():
	case <-time.After(500 * time.Millisecond):
		t.Fatal("ticker did not tick in time")
	}
	ticker.Stop()

	// 负数与零 interval 应返回 InvalidInput 错误而非 panic
	for _, interval := range []time.Duration{0, -1 * time.Second} {
		_, err := c.NewTicker(interval)
		if err == nil {
			t.Fatalf("expected error for interval %v, got nil", interval)
		}
		if !errors.Is(err, errors.InvalidInput) {
			t.Fatalf("expected InvalidInput error, got %v", err)
		}
	}
}
