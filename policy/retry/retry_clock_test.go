package retry

import (
	"context"
	"gochen/errors"
	"runtime"
	"testing"
	"time"

	"gochen/clock"
)

func TestDoWithInfo_UsesInjectedClock_ForBackoff(t *testing.T) {
	clk := clock.NewManualClock(time.Unix(0, 0).UTC())

	attemptCh := make(chan int, 10)
	done := make(chan error, 1)

	cfg := Config{
		MaxAttempts:   2,
		InitialDelay:  10 * time.Millisecond,
		BackoffFactor: 2,
		MaxDelay:      1 * time.Second,
		Clock:         clk,
	}

	go func() {
		err := DoWithInfo(context.Background(), func(ctx context.Context, attempt int) error {
			attemptCh <- attempt
			if attempt == 1 {
				return errors.New("fail once")
			}
			return nil
		}, cfg)
		done <- err
	}()

	waitAttempt(t, attemptCh, 1)

	// 未推进时间前，不应出现第二次尝试。
	select {
	case got := <-attemptCh:
		t.Fatalf("unexpected attempt=%d before Advance()", got)
	default:
	}

	advanceAndWaitAttempt(t, clk, attemptCh, 2, 10*time.Millisecond)

	if err := <-done; err != nil {
		t.Fatalf("expected success, got err: %v", err)
	}
}

func TestDoWithInfo_ContextCancelStopsInjectedClockBackoff(t *testing.T) {
	clk := clock.NewManualClock(time.Unix(0, 0).UTC())

	attemptCh := make(chan int, 10)
	done := make(chan error, 1)

	cfg := Config{
		MaxAttempts:   3,
		InitialDelay:  10 * time.Second,
		BackoffFactor: 2,
		MaxDelay:      10 * time.Second,
		Clock:         clk,
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		err := DoWithInfo(ctx, func(ctx context.Context, attempt int) error {
			_ = ctx
			attemptCh <- attempt
			return errors.New("retry")
		}, cfg)
		done <- err
	}()

	waitAttempt(t, attemptCh, 1)
	cancel()

	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for cancellation")
	}

	select {
	case got := <-attemptCh:
		t.Fatalf("unexpected attempt=%d after cancellation", got)
	default:
	}
}

func waitAttempt(t *testing.T, ch <-chan int, want int) {
	t.Helper()

	timeout := 5 * time.Second
	select {
	case got := <-ch:
		if got != want {
			t.Fatalf("attempt = %d, want %d", got, want)
		}
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for attempt %d", want)
	}
}

func advanceAndWaitAttempt(t *testing.T, clk *clock.ManualClock, ch <-chan int, want int, advance time.Duration) {
	t.Helper()

	timeout := 5 * time.Second

	start := time.Now()
	clk.Advance(advance)
	for {
		select {
		case got := <-ch:
			if got != want {
				t.Fatalf("attempt = %d, want %d", got, want)
			}
			return
		default:
			clk.Advance(1 * time.Millisecond)
			yield()
			if time.Since(start) > timeout {
				t.Fatalf("timed out waiting for attempt %d", want)
			}
		}
	}
}

func yield() {
	for i := 0; i < 16; i++ {
		runtime.Gosched()
	}
}
