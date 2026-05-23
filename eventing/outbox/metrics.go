package outbox

import (
	"context"
	"fmt"
	"time"

	"gochen/db"
	"gochen/errors"
)

// OutboxMetrics 表示一次采样得到的 Outbox 运行指标快照。
type OutboxMetrics struct {
	// 各状态的记录数
	PendingCount   int64 `json:"pending_count"`
	PublishedCount int64 `json:"published_count"`
	FailedCount    int64 `json:"failed_count"`

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

const (
	defaultMaxPendingCount         = 10000
	defaultMaxFailedCount          = 1000
	defaultMaxDLQCount             = 100
	defaultMaxPendingAge           = 1 * time.Hour
	maxAvgPublishDelaySampleSize   = 1000
	defaultHighRetryCountThreshold = 3
)

// MetricsCollector 通过 SQL 查询采集 Outbox 指标，并据此计算健康状态。
type MetricsCollector struct {
	db db.IDatabase

	// 健康阈值配置
	maxPendingCount int64         // 待处理记录数阈值
	maxFailedCount  int64         // 失败记录数阈值
	maxDLQCount     int64         // DLQ 记录数阈值
	maxPendingAge   time.Duration // 最老待处理记录年龄阈值
}

// NewMetricsCollector 使用默认阈值创建一个 Outbox 指标采集器。
func NewMetricsCollector(db db.IDatabase) IMetricsCollector {
	return &MetricsCollector{
		db:              db,
		maxPendingCount: defaultMaxPendingCount,
		maxFailedCount:  defaultMaxFailedCount,
		maxDLQCount:     defaultMaxDLQCount,
		maxPendingAge:   defaultMaxPendingAge,
	}
}

// NewMetricsCollectorWithThresholds 使用自定义阈值创建指标采集器。
func NewMetricsCollectorWithThresholds(
	db db.IDatabase,
	maxPendingCount, maxFailedCount, maxDLQCount int64,
	maxPendingAge time.Duration,
) IMetricsCollector {
	return &MetricsCollector{
		db:              db,
		maxPendingCount: maxPendingCount,
		maxFailedCount:  maxFailedCount,
		maxDLQCount:     maxDLQCount,
		maxPendingAge:   maxPendingAge,
	}
}

// Collect 汇总当前 Outbox 的状态计数、重试情况和时间维度指标。
func (c *MetricsCollector) Collect(ctx context.Context) (*OutboxMetrics, error) {
	metrics := &OutboxMetrics{
		CollectedAt: time.Now(),
	}

	// 1. 采集各状态的记录数
	if err := c.collectStatusCounts(ctx, metrics); err != nil {
		return nil, errors.Wrap(err, errors.Database, "collect status counts failed")
	}

	// 2. 采集 DLQ 记录数
	if err := c.collectDLQCount(ctx, metrics); err != nil {
		// DLQ 可能不存在，忽略错误
		metrics.DLQCount = 0
	}

	// 3. 采集重试统计
	if err := c.collectRetryStats(ctx, metrics); err != nil {
		return nil, errors.Wrap(err, errors.Database, "collect retry stats failed")
	}

	// 4. 采集时间统计
	if err := c.collectTimeStats(ctx, metrics); err != nil {
		return nil, errors.Wrap(err, errors.Database, "collect time stats failed")
	}

	return metrics, nil
}

// collectStatusCounts 统计 pending/published/failed 三类记录数量。
func (c *MetricsCollector) collectStatusCounts(ctx context.Context, metrics *OutboxMetrics) error {
	query := `
		SELECT 
			SUM(CASE WHEN status = 'pending' THEN 1 ELSE 0 END) as pending,
			SUM(CASE WHEN status = 'published' THEN 1 ELSE 0 END) as published,
			SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END) as failed
		FROM event_outbox
	`

	err := c.db.QueryRow(ctx, query).Scan(
		&metrics.PendingCount,
		&metrics.PublishedCount,
		&metrics.FailedCount,
	)
	if err != nil {
		return err
	}

	return nil
}

// collectDLQCount 统计死信表中的记录数量。
func (c *MetricsCollector) collectDLQCount(ctx context.Context, metrics *OutboxMetrics) error {
	query := `SELECT COUNT(*) FROM event_outbox_dlq`

	err := c.db.QueryRow(ctx, query).Scan(&metrics.DLQCount)
	if err != nil {
		return err
	}

	return nil
}

// collectRetryStats 汇总当前未完成记录的最大、平均和高频重试次数。
func (c *MetricsCollector) collectRetryStats(ctx context.Context, metrics *OutboxMetrics) error {
	query := `
		SELECT 
			COALESCE(MAX(retry_count), 0) as max_retry,
			COALESCE(AVG(retry_count), 0) as avg_retry,
			SUM(CASE WHEN retry_count > 3 THEN 1 ELSE 0 END) as high_retry
		FROM event_outbox
		WHERE status != 'published'
	`

	err := c.db.QueryRow(ctx, query).Scan(
		&metrics.MaxRetryCount,
		&metrics.AvgRetryCount,
		&metrics.HighRetryCount,
	)
	if err != nil {
		return err
	}

	return nil
}

// collectTimeStats 统计最老待处理记录年龄和平均发布时间延迟。
func (c *MetricsCollector) collectTimeStats(ctx context.Context, metrics *OutboxMetrics) error {
	// 最老待处理记录的年龄
	var oldestCreatedAt *time.Time
	// 说明：当前实现基于 MySQL 语法（包括 TIMESTAMPDIFF 等函数），
	// 主要用于默认 Outbox SQL 仓储的监控场景；若使用其他数据库方言，
	// 建议在业务侧提供自定义 MetricsCollector 实现或调整查询语句。

	query := `
		SELECT MIN(created_at)
		FROM event_outbox
		WHERE status = 'pending'
	`

	err := c.db.QueryRow(ctx, query).Scan(&oldestCreatedAt)
	if err != nil {
		return err
	}

	if oldestCreatedAt != nil {
		metrics.OldestPendingAge = time.Since(*oldestCreatedAt)
	}

	// 平均发布延迟
	query = fmt.Sprintf(`
		SELECT AVG(TIMESTAMPDIFF(SECOND, created_at, published_at))
		FROM event_outbox
		WHERE status = 'published' AND published_at IS NOT NULL
		LIMIT %d
	`, maxAvgPublishDelaySampleSize)

	var avgDelaySeconds float64
	err = c.db.QueryRow(ctx, query).Scan(&avgDelaySeconds)
	if err != nil {
		// 可能没有已发布记录
		return nil
	}

	metrics.AvgPublishDelay = time.Duration(avgDelaySeconds) * time.Second

	return nil
}

// HealthStatus 根据当前采样结果判断 Outbox 是否健康。
func (c *MetricsCollector) HealthStatus(ctx context.Context) (HealthStatus, string, error) {
	metrics, err := c.Collect(ctx)
	if err != nil {
		return HealthStatusUnhealthy, "failed to collect metrics", err
	}

	// 检查各项指标
	var issues []string

	// 1. 待处理记录数过多
	if metrics.PendingCount > c.maxPendingCount {
		issues = append(issues, fmt.Sprintf("pending count too high: %d (threshold: %d)",
			metrics.PendingCount, c.maxPendingCount))
	}

	// 2. 失败记录数过多
	if metrics.FailedCount > c.maxFailedCount {
		issues = append(issues, fmt.Sprintf("failed count too high: %d (threshold: %d)",
			metrics.FailedCount, c.maxFailedCount))
	}

	// 3. DLQ 记录数过多
	if metrics.DLQCount > c.maxDLQCount {
		issues = append(issues, fmt.Sprintf("DLQ count too high: %d (threshold: %d)",
			metrics.DLQCount, c.maxDLQCount))
	}

	// 4. 最老待处理记录年龄过大
	if metrics.OldestPendingAge > c.maxPendingAge {
		issues = append(issues, fmt.Sprintf("oldest pending age too old: %s (threshold: %s)",
			metrics.OldestPendingAge, c.maxPendingAge))
	}

	// 判断健康状态
	if len(issues) == 0 {
		return HealthStatusHealthy, "all metrics within thresholds", nil
	}

	if len(issues) <= 2 {
		return HealthStatusDegraded, fmt.Sprintf("degraded: %v", issues), nil
	}

	return HealthStatusUnhealthy, fmt.Sprintf("unhealthy: %v", issues), nil
}

// MetricsSnapshot 指标快照。
//
// 用于导出指标到外部监控系统（如 Prometheus）。
type MetricsSnapshot struct {
	Timestamp time.Time         `json:"timestamp"`
	Metrics   *OutboxMetrics    `json:"metrics"`
	Health    HealthStatus      `json:"health"`
	Issues    string            `json:"issues,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// TakeSnapshot 获取指标快照。
func (c *MetricsCollector) TakeSnapshot(ctx context.Context, labels map[string]string) (*MetricsSnapshot, error) {
	metrics, err := c.Collect(ctx)
	if err != nil {
		return nil, err
	}

	health, issues, err := c.HealthStatus(ctx)
	if err != nil {
		return nil, err
	}

	return &MetricsSnapshot{
		Timestamp: time.Now(),
		Metrics:   metrics,
		Health:    health,
		Issues:    issues,
		Labels:    labels,
	}, nil
}
