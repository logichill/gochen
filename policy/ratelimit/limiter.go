package ratelimit

import (
	"math"
	"sync"
	"time"

	"gochen/clock"
)

// Config 定义限流器配置。
type Config struct {
	// RequestsPerSecond 每秒补充的令牌数，支持小数（如 0.5 表示每分钟 30 次）。
	// <=0 表示不限流；NaN 和无穷值拒绝请求。
	RequestsPerSecond float64
	// BurstSize 突发容量（<=0 时使用向上取整的 RequestsPerSecond，最少 1）。
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

type bucketEntry struct {
	bucket   *tokenBucket
	lastSeen time.Time
}

// ILimiter 限流器契约接口。
type ILimiter interface {
	Allow(key string) bool
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
	if math.IsNaN(l.cfg.RequestsPerSecond) || math.IsInf(l.cfg.RequestsPerSecond, 0) {
		return false
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
			// 仅清理已补满的闲置桶，避免低速率在窗口过期时凭空恢复突发额度。
			if entry == nil || (entry.lastSeen.Before(expireBefore) && entry.bucket.available(now) >= entry.bucket.capacity) {
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

// Tokens 返回指定 key 当前可用的令牌数，不消耗令牌，也不延长闲置清理时间。
// 未访问过的 key 返回完整突发容量；不限流或无效配置返回 0。
func (l *Limiter) Tokens(key string) float64 {
	if l == nil || l.cfg.RequestsPerSecond <= 0 || math.IsNaN(l.cfg.RequestsPerSecond) || math.IsInf(l.cfg.RequestsPerSecond, 0) {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if entry := l.buckets[key]; entry != nil {
		return entry.bucket.available(l.clk.Now())
	}
	return burstCapacity(l.cfg.RequestsPerSecond, l.cfg.BurstSize)
}
