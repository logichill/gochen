package monitoring

import (
	"context"
	"time"
)

// Snapshot 是监控聚合快照（面向对外导出）。
type Snapshot struct {
	Timestamp time.Time    `json:"timestamp"`
	Health    HealthReport `json:"health"`
	Metrics   Summary      `json:"metrics"`

	SnapshotManager *SnapshotManagerSnapshot `json:"snapshot_manager,omitempty"`
	Cache           *CacheSnapshot           `json:"cache,omitempty"`
	Outbox          *OutboxSnapshot          `json:"outbox,omitempty"`
}

// ISnapshotStatsProvider 提供快照管理器相关统计（可选）。
type ISnapshotStatsProvider interface {
	SnapshotStats(ctx context.Context) (SnapshotManagerStats, error)
}

// SnapshotManagerStats 是快照管理器统计信息（强类型）。
type SnapshotManagerStats struct {
	TotalSnapshots int            `json:"total_snapshots"`
	Enabled        bool           `json:"enabled"`
	Frequency      int            `json:"frequency"`
	RetentionDays  float64        `json:"retention_days"`
	Strategy       string         `json:"strategy"`
	ByType         map[string]int `json:"by_type,omitempty"`
}

// SnapshotManagerSnapshot 是快照管理器快照（包含错误信息）。
type SnapshotManagerSnapshot struct {
	Stats SnapshotManagerStats `json:"stats,omitempty"`
	Error string               `json:"error,omitempty"`
}

// ICacheStatsProvider 提供缓存统计（可选）。
type ICacheStatsProvider interface {
	CacheStats() CacheStats
}

// CacheStats 是缓存统计信息（强类型）。
type CacheStats struct {
	Hits           int64   `json:"hits"`
	Misses         int64   `json:"misses"`
	HitRatePercent float64 `json:"hit_rate"`
	Evictions      int64   `json:"evictions"`
	Invalidations  int64   `json:"invalidations"`

	CacheSize  int     `json:"cache_size"`
	MaxSize    int     `json:"max_size"`
	TTLSeconds float64 `json:"ttl_seconds"`
}

// CacheSnapshot 是缓存快照。
type CacheSnapshot struct {
	Stats CacheStats `json:"stats"`
}
