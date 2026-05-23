package ratelimit

import (
	"sync"
	"testing"
	"time"

	"gochen/clock"
)

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

	if !limiter.Allow("k") || !limiter.Allow("k") {
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
