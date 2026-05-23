package monitoring

import (
	"sync/atomic"
	"time"
)

// Metrics 事件系统监控指标。
//
// 说明：
// - 这是一个“可读的内存指标快照”实现（不依赖第三方 metrics SDK）。
// - 所有字段均通过原子操作更新与读取，适合在框架内部作为默认埋点落点。
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

	// Outbox 指标（发布路径）
	OutboxDecodeCount    int64 // 反序列化条目次数
	OutboxDecodeErrors   int64 // 反序列化失败次数
	OutboxDecodeDuration int64 // 反序列化总耗时（纳秒）

	OutboxPublishCount    int64 // 发布尝试次数
	OutboxPublishErrors   int64 // 发布失败次数
	OutboxPublishDuration int64 // 发布总耗时（纳秒）

	startTimeUnixNano int64
}

// HasNonZeroSnapshot 判断指标是否已产生过观测值。
//
// 说明：
// - 该方法用于“可观测性提示”（例如 health/snapshot 中提示是否已装配并产生埋点）；
// - 这里不区分“未装配”与“装配了但还没产生数据”，调用方可按需结合业务语义解释。
func (m *Metrics) HasNonZeroSnapshot() bool {
	if m == nil {
		return false
	}
	s := m.Snapshot()
	return s.EventsSaved > 0 ||
		s.EventsLoaded > 0 ||
		s.EventStoreErrors > 0 ||
		s.SnapshotsCreated > 0 ||
		s.SnapshotsLoaded > 0 ||
		s.SnapshotHits > 0 ||
		s.SnapshotMisses > 0 ||
		s.EventsProcessed > 0 ||
		s.ProjectionUpdates > 0 ||
		s.CacheHits > 0 ||
		s.CacheMisses > 0 ||
		s.CacheEvictions > 0
}

// IEventStoreMetricsRecorder 抽象事件存储埋点（便于显式注入，避免隐式全局依赖）。
type IEventStoreMetricsRecorder interface {
	RecordEventSaved(count int, duration time.Duration)
	RecordEventLoaded(count int, duration time.Duration)
	RecordEventStoreError()
}

// ISnapshotMetricsRecorder 抽象快照埋点。
type ISnapshotMetricsRecorder interface {
	RecordSnapshotCreated(duration time.Duration)
	RecordSnapshotLoaded(duration time.Duration, hit bool)
}

// IProjectionMetricsRecorder 抽象投影埋点。
type IProjectionMetricsRecorder interface {
	RecordEventProcessed(duration time.Duration, success bool)
	RecordProjectionUpdate(success bool, lag time.Duration)
}

// ICacheMetricsRecorder 抽象缓存埋点。
type ICacheMetricsRecorder interface {
	RecordCacheHit()
	RecordCacheMiss()
	RecordCacheEviction()
}

// NewMetrics 创建一份基于原子计数的内存指标实现。
func NewMetrics() *Metrics {
	return &Metrics{startTimeUnixNano: time.Now().UnixNano()}
}

// RecordEventSaved 累计事件保存次数与耗时。
func (m *Metrics) RecordEventSaved(count int, duration time.Duration) {
	atomic.AddInt64(&m.EventsSaved, int64(count))
	atomic.AddInt64(&m.EventStoreDuration, int64(duration))
}

// RecordEventLoaded 累计事件加载次数与耗时。
func (m *Metrics) RecordEventLoaded(count int, duration time.Duration) {
	atomic.AddInt64(&m.EventsLoaded, int64(count))
	atomic.AddInt64(&m.EventLoadDuration, int64(duration))
}

// RecordEventStoreError 累计事件存储错误数。
func (m *Metrics) RecordEventStoreError() { atomic.AddInt64(&m.EventStoreErrors, 1) }

// RecordSnapshotCreated 累计快照创建次数与耗时。
func (m *Metrics) RecordSnapshotCreated(duration time.Duration) {
	atomic.AddInt64(&m.SnapshotsCreated, 1)
	atomic.AddInt64(&m.SnapshotDuration, int64(duration))
}

// RecordSnapshotLoaded 累计快照加载次数，并区分 hit/miss。
func (m *Metrics) RecordSnapshotLoaded(duration time.Duration, hit bool) {
	atomic.AddInt64(&m.SnapshotsLoaded, 1)
	atomic.AddInt64(&m.SnapshotDuration, int64(duration))
	if hit {
		atomic.AddInt64(&m.SnapshotHits, 1)
	} else {
		atomic.AddInt64(&m.SnapshotMisses, 1)
	}
}

// RecordEventProcessed 累计事件处理次数、耗时与错误数。
func (m *Metrics) RecordEventProcessed(duration time.Duration, success bool) {
	atomic.AddInt64(&m.EventsProcessed, 1)
	atomic.AddInt64(&m.EventProcessingTime, int64(duration))
	if !success {
		atomic.AddInt64(&m.EventProcessingErrors, 1)
	}
}

// RecordProjectionUpdate 累计投影更新次数，并刷新最近一次延迟值。
func (m *Metrics) RecordProjectionUpdate(success bool, lag time.Duration) {
	atomic.AddInt64(&m.ProjectionUpdates, 1)
	if !success {
		atomic.AddInt64(&m.ProjectionErrors, 1)
	}
	atomic.StoreInt64(&m.ProjectionLag, lag.Milliseconds())
}

// RecordCacheHit 累计缓存命中次数。
func (m *Metrics) RecordCacheHit() { atomic.AddInt64(&m.CacheHits, 1) }

// RecordCacheMiss 累计缓存未命中次数。
func (m *Metrics) RecordCacheMiss() { atomic.AddInt64(&m.CacheMisses, 1) }

// RecordCacheEviction 累计缓存驱逐次数。
func (m *Metrics) RecordCacheEviction() { atomic.AddInt64(&m.CacheEvictions, 1) }

// RecordOutboxDecode 累计发件箱反序列化次数、耗时与错误数。
func (m *Metrics) RecordOutboxDecode(d time.Duration, err bool) {
	atomic.AddInt64(&m.OutboxDecodeCount, 1)
	atomic.AddInt64(&m.OutboxDecodeDuration, int64(d))
	if err {
		atomic.AddInt64(&m.OutboxDecodeErrors, 1)
	}
}

// RecordOutboxPublish 累计发件箱发布次数、耗时与错误数。
func (m *Metrics) RecordOutboxPublish(d time.Duration, err bool) {
	atomic.AddInt64(&m.OutboxPublishCount, 1)
	atomic.AddInt64(&m.OutboxPublishDuration, int64(d))
	if err {
		atomic.AddInt64(&m.OutboxPublishErrors, 1)
	}
}

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

	// Outbox 指标
	OutboxDecodeCount    int64
	OutboxDecodeErrors   int64
	OutboxDecodeDuration time.Duration

	OutboxPublishCount    int64
	OutboxPublishErrors   int64
	OutboxPublishDuration time.Duration

	Uptime time.Duration
}

func (m *Metrics) Snapshot() MetricsSnapshot {
	start := time.Unix(0, atomic.LoadInt64(&m.startTimeUnixNano))
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

		OutboxDecodeCount:    atomic.LoadInt64(&m.OutboxDecodeCount),
		OutboxDecodeErrors:   atomic.LoadInt64(&m.OutboxDecodeErrors),
		OutboxDecodeDuration: time.Duration(atomic.LoadInt64(&m.OutboxDecodeDuration)),

		OutboxPublishCount:    atomic.LoadInt64(&m.OutboxPublishCount),
		OutboxPublishErrors:   atomic.LoadInt64(&m.OutboxPublishErrors),
		OutboxPublishDuration: time.Duration(atomic.LoadInt64(&m.OutboxPublishDuration)),

		Uptime: time.Since(start),
	}
}

// Reset 清零全部指标，并重置启动时间基线。
func (m *Metrics) Reset() {
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

	atomic.StoreInt64(&m.OutboxDecodeCount, 0)
	atomic.StoreInt64(&m.OutboxDecodeErrors, 0)
	atomic.StoreInt64(&m.OutboxDecodeDuration, 0)

	atomic.StoreInt64(&m.OutboxPublishCount, 0)
	atomic.StoreInt64(&m.OutboxPublishErrors, 0)
	atomic.StoreInt64(&m.OutboxPublishDuration, 0)

	atomic.StoreInt64(&m.startTimeUnixNano, time.Now().UnixNano())
}

// Summary 把完整指标快照压缩成适合 JSON 输出的稳定结构。
func (s MetricsSnapshot) Summary() Summary {
	return Summary{
		UptimeSeconds: s.Uptime.Seconds(),
		EventStore: EventStoreSummary{
			EventsSaved:         s.EventsSaved,
			EventsLoaded:        s.EventsLoaded,
			StoreDurationMillis: s.EventStoreDuration.Milliseconds(),
			LoadDurationMillis:  s.EventLoadDuration.Milliseconds(),
			Errors:              s.EventStoreErrors,
			AvgSaveDurationMS:   avgDurationMillis(s.EventStoreDuration, s.EventsSaved),
			AvgLoadDurationMS:   avgDurationMillis(s.EventLoadDuration, s.EventsLoaded),
			ErrorRatePercent:    errorRatePercent(s.EventStoreErrors, s.EventsSaved),
		},
		Snapshot: SnapshotSummary{
			Created:        s.SnapshotsCreated,
			Loaded:         s.SnapshotsLoaded,
			Hits:           s.SnapshotHits,
			Misses:         s.SnapshotMisses,
			DurationMillis: s.SnapshotDuration.Milliseconds(),
			HitRatePercent: hitRatePercent(s.SnapshotHits, s.SnapshotMisses),
			AvgDurationMS:  avgDurationMillis(s.SnapshotDuration, s.SnapshotsCreated+s.SnapshotsLoaded),
		},
		EventProcessing: EventProcessingSummary{
			Processed:        s.EventsProcessed,
			DurationMillis:   s.EventProcessingTime.Milliseconds(),
			Errors:           s.EventProcessingErrors,
			AvgDurationMS:    avgDurationMillis(s.EventProcessingTime, s.EventsProcessed),
			ErrorRatePercent: errorRatePercent(s.EventProcessingErrors, s.EventsProcessed),
			ThroughputPerSec: throughputPerSec(s.EventsProcessed, s.Uptime),
		},
		Projection: ProjectionSummary{
			Updates:          s.ProjectionUpdates,
			Errors:           s.ProjectionErrors,
			LagMillis:        s.ProjectionLag.Milliseconds(),
			ErrorRatePercent: errorRatePercent(s.ProjectionErrors, s.ProjectionUpdates),
		},
		Cache: CacheSummary{
			Hits:           s.CacheHits,
			Misses:         s.CacheMisses,
			Evictions:      s.CacheEvictions,
			HitRatePercent: hitRatePercent(s.CacheHits, s.CacheMisses),
		},
		Outbox: OutboxSummary{
			DecodeCount:             s.OutboxDecodeCount,
			DecodeErrors:            s.OutboxDecodeErrors,
			DecodeDurationMS:        s.OutboxDecodeDuration.Milliseconds(),
			AvgDecodeDurationMS:     avgDurationMillis(s.OutboxDecodeDuration, s.OutboxDecodeCount),
			PublishCount:            s.OutboxPublishCount,
			PublishErrors:           s.OutboxPublishErrors,
			PublishDurationMS:       s.OutboxPublishDuration.Milliseconds(),
			AvgPublishDurationMS:    avgDurationMillis(s.OutboxPublishDuration, s.OutboxPublishCount),
			PublishErrorRatePercent: errorRatePercent(s.OutboxPublishErrors, s.OutboxPublishCount),
		},
	}
}

// Summary 结构化指标输出（便于 JSON 导出）。
type Summary struct {
	UptimeSeconds   float64                `json:"uptime_seconds"`
	EventStore      EventStoreSummary      `json:"event_store"`
	Snapshot        SnapshotSummary        `json:"snapshot"`
	EventProcessing EventProcessingSummary `json:"event_processing"`
	Projection      ProjectionSummary      `json:"projection"`
	Cache           CacheSummary           `json:"cache"`
	Outbox          OutboxSummary          `json:"outbox"`
}

// EventStoreSummary 汇总事件存储相关指标。
type EventStoreSummary struct {
	EventsSaved         int64   `json:"events_saved"`
	EventsLoaded        int64   `json:"events_loaded"`
	StoreDurationMillis int64   `json:"store_duration_ms"`
	LoadDurationMillis  int64   `json:"load_duration_ms"`
	Errors              int64   `json:"errors"`
	AvgSaveDurationMS   float64 `json:"avg_save_duration_ms"`
	AvgLoadDurationMS   float64 `json:"avg_load_duration_ms"`
	ErrorRatePercent    float64 `json:"error_rate"`
}

// SnapshotSummary 汇总快照命中率和耗时指标。
type SnapshotSummary struct {
	Created        int64   `json:"created"`
	Loaded         int64   `json:"loaded"`
	Hits           int64   `json:"hits"`
	Misses         int64   `json:"misses"`
	DurationMillis int64   `json:"duration_ms"`
	HitRatePercent float64 `json:"hit_rate"`
	AvgDurationMS  float64 `json:"avg_duration_ms"`
}

// EventProcessingSummary 汇总事件处理吞吐、错误率和耗时指标。
type EventProcessingSummary struct {
	Processed        int64   `json:"processed"`
	DurationMillis   int64   `json:"duration_ms"`
	Errors           int64   `json:"errors"`
	AvgDurationMS    float64 `json:"avg_duration_ms"`
	ErrorRatePercent float64 `json:"error_rate"`
	ThroughputPerSec float64 `json:"throughput_per_sec"`
}

// ProjectionSummary 汇总投影更新与错误情况。
type ProjectionSummary struct {
	Updates          int64   `json:"updates"`
	Errors           int64   `json:"errors"`
	LagMillis        int64   `json:"lag_ms"`
	ErrorRatePercent float64 `json:"error_rate"`
}

// CacheSummary 汇总缓存命中、未命中和驱逐情况。
type CacheSummary struct {
	Hits           int64   `json:"hits"`
	Misses         int64   `json:"misses"`
	Evictions      int64   `json:"evictions"`
	HitRatePercent float64 `json:"hit_rate"`
}

// OutboxSummary 定义发件箱摘要。
type OutboxSummary struct {
	DecodeCount         int64   `json:"decode_count"`
	DecodeErrors        int64   `json:"decode_errors"`
	DecodeDurationMS    int64   `json:"decode_duration_ms"`
	AvgDecodeDurationMS float64 `json:"avg_decode_duration_ms"`

	PublishCount            int64   `json:"publish_count"`
	PublishErrors           int64   `json:"publish_errors"`
	PublishDurationMS       int64   `json:"publish_duration_ms"`
	AvgPublishDurationMS    float64 `json:"avg_publish_duration_ms"`
	PublishErrorRatePercent float64 `json:"publish_error_rate"`
}

// avgDurationMillis 计算每次操作的平均耗时（毫秒）。
func avgDurationMillis(total time.Duration, count int64) float64 {
	if count == 0 {
		return 0
	}
	return float64(total.Milliseconds()) / float64(count)
}

// errorRatePercent 计算错误数占总次数的百分比。
func errorRatePercent(errors, total int64) float64 {
	if total == 0 {
		return 0
	}
	return float64(errors) / float64(total) * 100
}

// hitRatePercent 计算命中次数占总访问次数的百分比。
func hitRatePercent(hits, misses int64) float64 {
	total := hits + misses
	if total == 0 {
		return 0
	}
	return float64(hits) / float64(total) * 100
}

// throughputPerSec 计算在给定运行时长下的平均每秒吞吐。
func throughputPerSec(count int64, duration time.Duration) float64 {
	if duration.Seconds() == 0 {
		return 0
	}
	return float64(count) / duration.Seconds()
}
