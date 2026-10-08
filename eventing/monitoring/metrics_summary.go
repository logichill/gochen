package monitoring

import (
	"time"
)

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
