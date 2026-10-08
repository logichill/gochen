package monitoring

import (
	"time"
)

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
