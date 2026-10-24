package ratelimit

import (
	"math"
	"sync"
	"time"

	"gochen/clock"
)

type tokenBucket struct {
	clk clock.IClock

	rate       float64
	capacity   float64
	tokens     float64
	lastRefill time.Time
	mu         sync.Mutex
}

// newTokenBucket 创建令牌Bucket。
func newTokenBucket(clk clock.IClock, rps float64, burst int) *tokenBucket {
	if clk == nil {
		clk = clock.NewRealClock()
	}
	if rps <= 0 {
		rps = 1
	}
	capacity := burstCapacity(rps, burst)
	return &tokenBucket{
		clk:        clk,
		rate:       rps,
		capacity:   capacity,
		tokens:     capacity,
		lastRefill: clk.Now(),
	}
}

// allow 判断条件是否成立。
func (b *tokenBucket) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.refillLocked(b.clk.Now())
	if b.tokens >= 1.0 {
		b.tokens -= 1.0
		return true
	}
	return false
}

func burstCapacity(rps float64, burst int) float64 {
	if burst > 0 {
		return float64(burst)
	}
	return math.Max(1, math.Ceil(rps))
}

func (b *tokenBucket) refillLocked(now time.Time) {
	elapsed := now.Sub(b.lastRefill).Seconds()
	if elapsed > 0 {
		b.tokens += elapsed * b.rate
		if b.tokens > b.capacity {
			b.tokens = b.capacity
		}
		b.lastRefill = now
	}
}

func (b *tokenBucket) available(now time.Time) float64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refillLocked(now)
	return b.tokens
}
