package ratelimit

import (
	"math"
	"sync"
	"testing"
	"time"

	"gochen/clock"
)

func TestLimiterFractionalRateAndTokenSnapshot(t *testing.T) {
	clk := clock.NewManualClock(time.Unix(0, 0))
	l := New(Config{RequestsPerSecond: 0.5, Clock: clk})
	if l.Tokens("user") != 1 || !l.Allow("user") || l.Allow("user") {
		t.Fatal("expected one initial token")
	}
	clk.Advance(time.Second)
	if l.Tokens("user") != 0.5 || l.Allow("user") {
		t.Fatal("one second must only refill half a token")
	}
	clk.Advance(time.Second)
	if !l.Allow("user") || l.Allow("user") {
		t.Fatal("two seconds must refill exactly one token")
	}
	if !l.Allow("other-user") {
		t.Fatal("different keys must have independent buckets")
	}
}

func TestLimiterIdleCleanupDoesNotResetSlowBucket(t *testing.T) {
	clk := clock.NewManualClock(time.Unix(0, 0))
	l := New(Config{RequestsPerSecond: 1.0 / 3600, BurstSize: 1, WindowSize: time.Minute, Clock: clk})
	if !l.Allow("user") {
		t.Fatal("initial request denied")
	}
	clk.Advance(2 * time.Minute)
	if l.Allow("user") {
		t.Fatal("idle cleanup reset the hourly quota")
	}
	clk.Advance(time.Hour)
	if !l.Allow("user") {
		t.Fatal("hourly quota did not refill")
	}
}

func TestLimiterRejectsNonFiniteRate(t *testing.T) {
	for _, rate := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		l := New(Config{RequestsPerSecond: rate})
		if l.Allow("user") || l.Tokens("user") != 0 {
			t.Fatalf("invalid rate allowed: %v", rate)
		}
	}
}

func TestLimiter_Allow_UnlimitedWhenRPSIsNonPositive(t *testing.T) {
	limiter := New(Config{RequestsPerSecond: 0})
	for i := 0; i < 1000; i++ {
		if !limiter.Allow("k") {
			t.Fatalf("expected allow=true when rps<=0, i=%d", i)
		}
	}
}

func TestLimiter_Allow_UsesBurstCapacity(t *testing.T) {
	limiter := New(Config{RequestsPerSecond: 1, BurstSize: 2})

	first := limiter.Allow("k")
	second := limiter.Allow("k")
	if !first || !second {
		t.Fatalf("expected first two requests to fit burst")
	}
	if limiter.Allow("k") {
		t.Fatalf("expected third request to exceed burst")
	}
}

func TestLimiter_Allow_RefillsWithClock(t *testing.T) {
	clk := clock.NewManualClock(time.Unix(0, 0).UTC())
	limiter := New(Config{
		RequestsPerSecond: 2,
		BurstSize:         1,
		Clock:             clk,
	})

	if !limiter.Allow("k") {
		t.Fatalf("expected initial token to allow")
	}
	if limiter.Allow("k") {
		t.Fatalf("expected exhausted bucket to reject")
	}
	clk.Advance(500 * time.Millisecond)
	if !limiter.Allow("k") {
		t.Fatalf("expected half-second refill at 2 rps to allow")
	}
}

func TestLimiter_Allow_CleansIdleBucketsByWindowSize(t *testing.T) {
	clk := clock.NewManualClock(time.Unix(0, 0).UTC())
	limiter := New(Config{
		RequestsPerSecond: 1,
		BurstSize:         1,
		WindowSize:        time.Second,
		Clock:             clk,
	})

	if !limiter.Allow("old") {
		t.Fatalf("expected initial old key to allow")
	}
	clk.Advance(2 * time.Second)
	if !limiter.Allow("new") {
		t.Fatalf("expected new key to allow")
	}

	limiter.mu.Lock()
	_, oldExists := limiter.buckets["old"]
	_, newExists := limiter.buckets["new"]
	limiter.mu.Unlock()

	if oldExists {
		t.Fatalf("expected idle old bucket to be cleaned")
	}
	if !newExists {
		t.Fatalf("expected new bucket to remain")
	}
}

func TestLimiter_Allow_Concurrent(t *testing.T) {
	limiter := New(Config{RequestsPerSecond: 1000, BurstSize: 1000})

	var wg sync.WaitGroup
	results := make(chan bool, 100)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- limiter.Allow("k")
		}()
	}
	wg.Wait()
	close(results)

	allowed := 0
	for ok := range results {
		if ok {
			allowed++
		}
	}
	if allowed != 100 {
		t.Fatalf("allowed = %d, want 100", allowed)
	}
}
