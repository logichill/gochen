package ratelimit

import (
	"math"
	"reflect"
	"sort"
	"sync"
	"time"

	"gochen/clock"
)

// defaultMaxKeys 未经配置时单实例最多跟踪的 key 数量。
const defaultMaxKeys = 10000

// Config 定义限流器配置。
type Config struct {
	// RequestsPerSecond 每秒补充的令牌数，支持小数（如 0.5 表示每分钟 30 次）。
	// <=0 表示不限流；NaN 和无穷值拒绝请求。
	RequestsPerSecond float64
	// BurstSize 突发容量（<=0 时使用向上取整的 RequestsPerSecond，最少 1）。
	BurstSize int
	// WindowSize 用于 key 维度的“闲置清理”窗口（<=0 默认 1 分钟）。
	WindowSize time.Duration
	// MaxKeys 单实例最多跟踪的 key 数量上限（<=0 使用 defaultMaxKeys）。
	// 高基数 key（伪造 IP、随机请求 ID、恶意探测）场景下，超出上限时按
	// 最早访问时间淘汰，避免 buckets 无界膨胀导致 OOM。
	MaxKeys int

	// Clock 可选：用于 token refill 与 bucket 清理的时间来源，便于测试稳定推进时间。
	Clock clock.IClock
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
	if cfg.MaxKeys <= 0 {
		cfg.MaxKeys = defaultMaxKeys
	}
	if cfg.Clock == nil || isNilClock(cfg.Clock) {
		cfg.Clock = clock.NewRealClock()
	}
	return &Limiter{
		cfg:         cfg,
		clk:         cfg.Clock,
		buckets:     make(map[string]*bucketEntry),
		lastCleanup: cfg.Clock.Now(),
	}
}

// isNilClock 识别接口内携带的 typed-nil 时钟，避免运行期调用 Now() 时空指针 panic。
func isNilClock(clk clock.IClock) bool {
	rv := reflect.ValueOf(clk)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		return rv.IsNil()
	default:
		return false
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
		// 容量上限：超出时按最早访问（最久未活跃）淘汰一批，保证内存有界。
		if len(l.buckets) > l.cfg.MaxKeys {
			l.evictOldestLocked(now, len(l.buckets)-l.cfg.MaxKeys)
		}
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

// evictOldestLocked 按 lastSeen 最久优先淘汰 count 个桶。
// 必须在持有 l.mu 的前提下调用。批量淘汰以摊薄排序扫描开销，
// 淘汰的重建桶将获得全新的突发额度，与首次注册语义一致。
func (l *Limiter) evictOldestLocked(now time.Time, count int) {
	if count <= 0 || len(l.buckets) == 0 {
		return
	}
	type evictionCandidate struct {
		key      string
		lastSeen time.Time
	}
	candidates := make([]evictionCandidate, 0, len(l.buckets))
	for k, entry := range l.buckets {
		lastSeen := now
		if entry != nil {
			lastSeen = entry.lastSeen
		}
		candidates = append(candidates, evictionCandidate{key: k, lastSeen: lastSeen})
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].lastSeen.Before(candidates[j].lastSeen)
	})
	if count > len(candidates) {
		count = len(candidates)
	}
	for _, c := range candidates[:count] {
		delete(l.buckets, c.key)
	}
}
