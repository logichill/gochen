package outbox

import (
	"context"
	"time"
)

// OutboxMetrics 表示一次采样得到的 Outbox 运行指标快照。
type OutboxMetrics struct {
	// 各状态的记录数
	PendingCount      int64 `json:"pending_count"`
	PublishedCount    int64 `json:"published_count"`
	FailedCount       int64 `json:"failed_count"`
	DeadLetteredCount int64 `json:"dead_lettered_count"`

	// DLQ 记录数
	DLQCount int64 `json:"dlq_count"`

	// 重试统计
	MaxRetryCount  int     `json:"max_retry_count"`  // 最大重试次数
	AvgRetryCount  float64 `json:"avg_retry_count"`  // 平均重试次数
	HighRetryCount int64   `json:"high_retry_count"` // 重试次数 > 3 的记录数

	// 时间统计
	OldestPendingAge time.Duration `json:"oldest_pending_age"` // 最老待处理记录的年龄
	AvgPublishDelay  time.Duration `json:"avg_publish_delay"`  // 平均发布延迟

	// 采集时间
	CollectedAt time.Time `json:"collected_at"`
}

// IMetricsCollector 定义采集 Outbox 指标与健康状态的最小能力。
type IMetricsCollector interface {
	// Collect 采集当前 Outbox 指标快照。
	Collect(ctx context.Context) (*OutboxMetrics, error)

	// HealthStatus 基于当前指标给出健康状态和说明信息。
	HealthStatus(ctx context.Context) (HealthStatus, string, error)
}

// HealthStatus 表示 Outbox 指标推导出的健康等级。
type HealthStatus string

const (
	// HealthStatusHealthy 表示指标处于健康范围内。
	HealthStatusHealthy HealthStatus = "healthy" // 健康
	// HealthStatusDegraded 表示系统仍可用，但已出现需要关注的风险。
	HealthStatusDegraded HealthStatus = "degraded" // 降级
	// HealthStatusUnhealthy 表示指标已经超出可接受范围。
	HealthStatusUnhealthy HealthStatus = "unhealthy" // 不健康
)

// MetricsSnapshot 指标快照。
type MetricsSnapshot struct {
	Timestamp time.Time         `json:"timestamp"`
	Metrics   *OutboxMetrics    `json:"metrics"`
	Health    HealthStatus      `json:"health"`
	Issues    string            `json:"issues,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
}
