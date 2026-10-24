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
	//
	// 量纲说明（错误率口径）：
	//   - EventsSaved/EventsLoaded 按“事件条数”累加（一次批量调用 +N），
	//     用于吞吐与平均耗时；
	//   - EventStoreSaves/EventStoreLoads 按“调用次数”累加（一次批量调用 +1），
	//     与 EventStoreErrors（失败调用次数）同量纲，供错误率计算使用；
	//   - 事件存储错误率 = EventStoreErrors / (EventStoreSaves + EventStoreLoads
	//     + EventStoreErrors)，分子分母均为调用次数。
	EventsSaved        int64 // 成功落库的事件条数
	EventsLoaded       int64 // 成功加载的事件条数
	EventStoreSaves    int64 // 成功的保存调用次数（一次批量保存计 1 次）
	EventStoreLoads    int64 // 成功的加载调用次数（一次批量加载计 1 次）
	EventStoreDuration int64 // 事件存储总耗时（纳秒）
	EventLoadDuration  int64 // 事件加载总耗时（纳秒）
	EventStoreErrors   int64 // 事件存储失败调用次数（读写路径均计入）

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
	ProjectionLag     int64 // 投影延迟（纳秒）

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

// NewMetrics 创建一份基于原子计数的内存指标实现。
func NewMetrics() *Metrics {
	return &Metrics{startTimeUnixNano: time.Now().UnixNano()}
}

// RecordEventSaved 累计一次成功的事件保存调用（count 为本批事件条数）与耗时。
// 除事件条数外同时按调用次数记账（EventStoreSaves +1），供错误率按统一的
// “调用次数”量纲计算；条数口径保留给吞吐与平均耗时指标。
func (m *Metrics) RecordEventSaved(count int, duration time.Duration) {
	atomic.AddInt64(&m.EventsSaved, int64(count))
	atomic.AddInt64(&m.EventStoreSaves, 1)
	atomic.AddInt64(&m.EventStoreDuration, int64(duration))
}

// RecordEventLoaded 累计一次成功的事件加载调用（count 为本批事件条数）与耗时。
// 调用次数记账语义同 RecordEventSaved。
func (m *Metrics) RecordEventLoaded(count int, duration time.Duration) {
	atomic.AddInt64(&m.EventsLoaded, int64(count))
	atomic.AddInt64(&m.EventStoreLoads, 1)
	atomic.AddInt64(&m.EventLoadDuration, int64(duration))
}

// RecordEventStoreError 累计一次失败的事件存储调用（读写路径共用）。
func (m *Metrics) RecordEventStoreError() { atomic.AddInt64(&m.EventStoreErrors, 1) }

// EventStoreErrorRatePercent 返回事件存储操作失败率（%，按调用次数量纲）：
// 分子 = 失败调用次数，分母 = 成功保存调用 + 成功加载调用 + 失败调用。
func (m *Metrics) EventStoreErrorRatePercent() float64 {
	if m == nil {
		return 0
	}
	total := atomic.LoadInt64(&m.EventStoreSaves) + atomic.LoadInt64(&m.EventStoreLoads) + atomic.LoadInt64(&m.EventStoreErrors)
	if total == 0 {
		return 0
	}
	return float64(atomic.LoadInt64(&m.EventStoreErrors)) / float64(total) * 100
}

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
	atomic.StoreInt64(&m.ProjectionLag, int64(lag))
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

func (m *Metrics) Snapshot() MetricsSnapshot {
	start := time.Unix(0, atomic.LoadInt64(&m.startTimeUnixNano))
	return MetricsSnapshot{
		EventsSaved:        atomic.LoadInt64(&m.EventsSaved),
		EventsLoaded:       atomic.LoadInt64(&m.EventsLoaded),
		EventStoreSaves:    atomic.LoadInt64(&m.EventStoreSaves),
		EventStoreLoads:    atomic.LoadInt64(&m.EventStoreLoads),
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
		ProjectionLag:     time.Duration(atomic.LoadInt64(&m.ProjectionLag)),

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
	atomic.StoreInt64(&m.EventStoreSaves, 0)
	atomic.StoreInt64(&m.EventStoreLoads, 0)
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
