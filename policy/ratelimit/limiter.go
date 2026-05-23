package ratelimit

import (
	"sync"
	"time"

	"gochen/clock"
)

// Config 定义限流器配置。
type Config struct {
	// RequestsPerSecond 每秒允许请求数（<=0 表示不限流）。
	RequestsPerSecond int
	// BurstSize 突发容量（<=0 时按 RequestsPerSecond 兜底，仍 <=0 则默认 1）。
	BurstSize int
	// WindowSize 用于 key 维度的“闲置清理”窗口（<=0 默认 1 分钟）。
	WindowSize time.Duration

	// Clock 可选：用于 token refill 与 bucket 清理的时间来源，便于测试稳定推进时间。
	Clock clock.IClock
}

type tokenBucket struct {
	clk clock.IClock

	rate       float64
	capacity   float64
	tokens     float64
	lastRefill time.Time
	mu         sync.Mutex
}

// newTokenBucket 创建令牌Bucket。
func newTokenBucket(clk clock.IClock, rps int, burst int) *tokenBucket {
	if clk == nil {
		clk = clock.NewRealClock()
	}
	if rps <= 0 {
		rps = 1
	}
	if burst <= 0 {
		burst = rps
	}
	return &tokenBucket{
		clk:        clk,
		rate:       float64(rps),
		capacity:   float64(burst),
		tokens:     float64(burst),
		lastRefill: clk.Now(),
	}
}

// allow 判断条件是否成立。
func (b *tokenBucket) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := b.clk.Now()
	elapsed := now.Sub(b.lastRefill).Seconds()
	if elapsed > 0 {
		b.tokens += elapsed * b.rate
		if b.tokens > b.capacity {
			b.tokens = b.capacity
		}
		b.lastRefill = now
	}

	if b.tokens >= 1.0 {
		b.tokens -= 1.0
		return true
	}
	return false
}

type bucketEntry struct {
	bucket   *tokenBucket
	lastSeen time.Time
}

// Limiter 定义Limiter。
type Limiter struct {
	cfg Config

	clk clock.IClock

	mu          sync.Mutex
	buckets     map[string]*bucketEntry
	lastCleanup time.Time
}

// New 创建限流器。
func New(cfg Config) *Limiter {
	if cfg.WindowSize <= 0 {
		cfg.WindowSize = time.Minute
	}
	if cfg.Clock == nil {
		cfg.Clock = clock.NewRealClock()
	}
	return &Limiter{
		cfg:         cfg,
		clk:         cfg.Clock,
		buckets:     make(map[string]*bucketEntry),
		lastCleanup: cfg.Clock.Now(),
	}
}

// Allow 判断指定 key 的请求是否允许通过。
func (l *Limiter) Allow(key string) bool {
	if l == nil {
		return true
	}
	if l.cfg.RequestsPerSecond <= 0 {
		return true
	}

	now := l.clk.Now()
	cleanupInterval := l.cfg.WindowSize
	if cleanupInterval <= 0 {
		cleanupInterval = time.Minute
	}

	l.mu.Lock()
	if now.Sub(l.lastCleanup) >= cleanupInterval {
		expireBefore := now.Add(-cleanupInterval)
		for k, entry := range l.buckets {
			if entry == nil || entry.lastSeen.Before(expireBefore) {
				delete(l.buckets, k)
			}
		}
		l.lastCleanup = now
	}

	entry := l.buckets[key]
	if entry == nil {
		entry = &bucketEntry{
			bucket:   newTokenBucket(l.clk, l.cfg.RequestsPerSecond, l.cfg.BurstSize),
			lastSeen: now,
		}
		l.buckets[key] = entry
	} else {
		entry.lastSeen = now
	}
	bucket := entry.bucket
	l.mu.Unlock()

	if bucket == nil {
		return true
	}
	return bucket.allow()
}
