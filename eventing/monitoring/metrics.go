package monitoring

import (
	"sync"
	"sync/atomic"
	"time"
)

// CachedStore 缓存存储接口，用于监控
type CachedStore interface {
	// 可以添加需要监控的缓存方法
	GetCacheStats() (hits, misses int64)
}

// Metrics 事件系统监控指标
type Metrics struct {
	// 事件存储指标
	EventsSaved        int64 // 保存的事件总数
	EventsLoaded       int64 // 加载的事件总数
	EventStoreDuration int64 // 事件存储总耗时（纳秒）
	EventLoadDuration  int64 // 事件加载总耗时（纳秒）
	EventStoreErrors   int64 // 事件存储错误数

	// 快照指标
	SnapshotsCreated int64 // 创建的快照总数
	SnapshotsLoaded  int64 // 加载的快照总数
	SnapshotHits     int64 // 快照命中次数
	SnapshotMisses   int64 // 快照未命中次数
	SnapshotDuration int64 // 快照操作总耗时（纳秒）

	// 事件处理指标
	EventsProcessed       int64 // 处理的事件总数
	EventProcessingTime   int64 // 事件处理总耗时（纳秒）
	EventProcessingErrors int64 // 事件处理错误数

	// 投影指标
	ProjectionUpdates int64 // 投影更新次数
	ProjectionErrors  int64 // 投影错误数
	ProjectionLag     int64 // 投影延迟（毫秒）

	// 缓存指标（如果启用）
	CacheHits      int64 // 缓存命中次数
	CacheMisses    int64 // 缓存未命中次数
	CacheEvictions int64 // 缓存驱逐次数

	startTime time.Time
	mutex     sync.RWMutex
}

// NewMetrics 创建新的指标收集器
func NewMetrics() *Metrics {
	return &Metrics{startTime: time.Now()}
}

// RecordEventSaved 记录事件保存
func (m *Metrics) RecordEventSaved(count int, duration time.Duration) {
	atomic.AddInt64(&m.EventsSaved, int64(count))
	atomic.AddInt64(&m.EventStoreDuration, int64(duration))
}

// RecordEventLoaded 记录事件加载
func (m *Metrics) RecordEventLoaded(count int, duration time.Duration) {
	atomic.AddInt64(&m.EventsLoaded, int64(count))
	atomic.AddInt64(&m.EventLoadDuration, int64(duration))
}

// RecordEventStoreError 记录事件存储错误
func (m *Metrics) RecordEventStoreError() { atomic.AddInt64(&m.EventStoreErrors, 1) }

// RecordSnapshotCreated 记录快照创建
func (m *Metrics) RecordSnapshotCreated(duration time.Duration) {
	atomic.AddInt64(&m.SnapshotsCreated, 1)
	atomic.AddInt64(&m.SnapshotDuration, int64(duration))
}

// RecordSnapshotLoaded 记录快照加载
func (m *Metrics) RecordSnapshotLoaded(duration time.Duration, hit bool) {
	atomic.AddInt64(&m.SnapshotsLoaded, 1)
	atomic.AddInt64(&m.SnapshotDuration, int64(duration))
	if hit {
		atomic.AddInt64(&m.SnapshotHits, 1)
	} else {
		atomic.AddInt64(&m.SnapshotMisses, 1)
	}
}

// RecordEventProcessed 记录事件处理
func (m *Metrics) RecordEventProcessed(duration time.Duration, success bool) {
	atomic.AddInt64(&m.EventsProcessed, 1)
	atomic.AddInt64(&m.EventProcessingTime, int64(duration))
	if !success {
		atomic.AddInt64(&m.EventProcessingErrors, 1)
	}
}

// RecordProjectionUpdate 记录投影更新
func (m *Metrics) RecordProjectionUpdate(success bool, lag time.Duration) {
	atomic.AddInt64(&m.ProjectionUpdates, 1)
	if !success {
		atomic.AddInt64(&m.ProjectionErrors, 1)
	}
	atomic.StoreInt64(&m.ProjectionLag, lag.Milliseconds())
}

// RecordCacheHit 记录缓存命中
func (m *Metrics) RecordCacheHit() { atomic.AddInt64(&m.CacheHits, 1) }

// RecordCacheMiss 记录缓存未命中
func (m *Metrics) RecordCacheMiss() { atomic.AddInt64(&m.CacheMisses, 1) }

// RecordCacheEviction 记录缓存驱逐
func (m *Metrics) RecordCacheEviction() { atomic.AddInt64(&m.CacheEvictions, 1) }

// MetricsSnapshot 指标快照（用于读取）
type MetricsSnapshot struct {
	// 事件存储指标
	EventsSaved        int64
	EventsLoaded       int64
	EventStoreDuration time.Duration
	EventLoadDuration  time.Duration
	EventStoreErrors   int64

	// 快照指标
	SnapshotsCreated int64
	SnapshotsLoaded  int64
	SnapshotHits     int64
	SnapshotMisses   int64
	SnapshotDuration time.Duration

	// 事件处理指标
	EventsProcessed       int64
	EventProcessingTime   time.Duration
	EventProcessingErrors int64

	// 投影指标
	ProjectionUpdates int64
	ProjectionErrors  int64
	ProjectionLag     time.Duration

	// 缓存指标
	CacheHits      int64
	CacheMisses    int64
	CacheEvictions int64

	Uptime time.Duration
}

// GetSnapshot 获取当前指标快照
func (m *Metrics) GetSnapshot() MetricsSnapshot {
	return MetricsSnapshot{
		EventsSaved:        atomic.LoadInt64(&m.EventsSaved),
		EventsLoaded:       atomic.LoadInt64(&m.EventsLoaded),
		EventStoreDuration: time.Duration(atomic.LoadInt64(&m.EventStoreDuration)),
		EventLoadDuration:  time.Duration(atomic.LoadInt64(&m.EventLoadDuration)),
		EventStoreErrors:   atomic.LoadInt64(&m.EventStoreErrors),

		SnapshotsCreated: atomic.LoadInt64(&m.SnapshotsCreated),
		SnapshotsLoaded:  atomic.LoadInt64(&m.SnapshotsLoaded),
		SnapshotHits:     atomic.LoadInt64(&m.SnapshotHits),
		SnapshotMisses:   atomic.LoadInt64(&m.SnapshotMisses),
		SnapshotDuration: time.Duration(atomic.LoadInt64(&m.SnapshotDuration)),

		EventsProcessed:       atomic.LoadInt64(&m.EventsProcessed),
		EventProcessingTime:   time.Duration(atomic.LoadInt64(&m.EventProcessingTime)),
		EventProcessingErrors: atomic.LoadInt64(&m.EventProcessingErrors),

		ProjectionUpdates: atomic.LoadInt64(&m.ProjectionUpdates),
		ProjectionErrors:  atomic.LoadInt64(&m.ProjectionErrors),
		ProjectionLag:     time.Duration(atomic.LoadInt64(&m.ProjectionLag)) * time.Millisecond,

		CacheHits:      atomic.LoadInt64(&m.CacheHits),
		CacheMisses:    atomic.LoadInt64(&m.CacheMisses),
		CacheEvictions: atomic.LoadInt64(&m.CacheEvictions),

		Uptime: time.Since(m.startTime),
	}
}

// Reset 重置所有指标
func (m *Metrics) Reset() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	atomic.StoreInt64(&m.EventsSaved, 0)
	atomic.StoreInt64(&m.EventsLoaded, 0)
	atomic.StoreInt64(&m.EventStoreDuration, 0)
	atomic.StoreInt64(&m.EventLoadDuration, 0)
	atomic.StoreInt64(&m.EventStoreErrors, 0)

	atomic.StoreInt64(&m.SnapshotsCreated, 0)
	atomic.StoreInt64(&m.SnapshotsLoaded, 0)
	atomic.StoreInt64(&m.SnapshotHits, 0)
	atomic.StoreInt64(&m.SnapshotMisses, 0)
	atomic.StoreInt64(&m.SnapshotDuration, 0)

	atomic.StoreInt64(&m.EventsProcessed, 0)
	atomic.StoreInt64(&m.EventProcessingTime, 0)
	atomic.StoreInt64(&m.EventProcessingErrors, 0)

	atomic.StoreInt64(&m.ProjectionUpdates, 0)
	atomic.StoreInt64(&m.ProjectionErrors, 0)
	atomic.StoreInt64(&m.ProjectionLag, 0)

	atomic.StoreInt64(&m.CacheHits, 0)
	atomic.StoreInt64(&m.CacheMisses, 0)
	atomic.StoreInt64(&m.CacheEvictions, 0)

	m.startTime = time.Now()
}

// ToMap 转换为map格式（便于JSON序列化）
func (s MetricsSnapshot) ToMap() map[string]any {
	result := map[string]any{
		"uptime_seconds": s.Uptime.Seconds(),
		"event_store": map[string]any{
			"events_saved":         s.EventsSaved,
			"events_loaded":        s.EventsLoaded,
			"store_duration_ms":    s.EventStoreDuration.Milliseconds(),
			"load_duration_ms":     s.EventLoadDuration.Milliseconds(),
			"errors":               s.EventStoreErrors,
			"avg_save_duration_ms": avgDuration(s.EventStoreDuration, s.EventsSaved),
			"avg_load_duration_ms": avgDuration(s.EventLoadDuration, s.EventsLoaded),
			"error_rate":           errorRate(s.EventStoreErrors, s.EventsSaved),
		},
		"snapshot": map[string]any{
			"created":         s.SnapshotsCreated,
			"loaded":          s.SnapshotsLoaded,
			"hits":            s.SnapshotHits,
			"misses":          s.SnapshotMisses,
			"duration_ms":     s.SnapshotDuration.Milliseconds(),
			"hit_rate":        hitRate(s.SnapshotHits, s.SnapshotMisses),
			"avg_duration_ms": avgDuration(s.SnapshotDuration, s.SnapshotsCreated+s.SnapshotsLoaded),
		},
		"event_processing": map[string]any{
			"processed":          s.EventsProcessed,
			"duration_ms":        s.EventProcessingTime.Milliseconds(),
			"errors":             s.EventProcessingErrors,
			"avg_duration_ms":    avgDuration(s.EventProcessingTime, s.EventsProcessed),
			"error_rate":         errorRate(s.EventProcessingErrors, s.EventsProcessed),
			"throughput_per_sec": throughput(s.EventsProcessed, s.Uptime),
		},
		"projection": map[string]any{
			"updates":    s.ProjectionUpdates,
			"errors":     s.ProjectionErrors,
			"lag_ms":     s.ProjectionLag.Milliseconds(),
			"error_rate": errorRate(s.ProjectionErrors, s.ProjectionUpdates),
		},
		"cache": map[string]any{
			"hits":      s.CacheHits,
			"misses":    s.CacheMisses,
			"evictions": s.CacheEvictions,
			"hit_rate":  hitRate(s.CacheHits, s.CacheMisses),
		},
	}
	return result
}

// helpers
func avgDuration(total time.Duration, count int64) float64 {
	if count == 0 {
		return 0
	}
	return float64(total.Milliseconds()) / float64(count)
}
func errorRate(errors, total int64) float64 {
	if total == 0 {
		return 0
	}
	return float64(errors) / float64(total) * 100
}
func hitRate(hits, misses int64) float64 {
	total := hits + misses
	if total == 0 {
		return 0
	}
	return float64(hits) / float64(total) * 100
}
func throughput(count int64, duration time.Duration) float64 {
	if duration.Seconds() == 0 {
		return 0
	}
	return float64(count) / duration.Seconds()
}
