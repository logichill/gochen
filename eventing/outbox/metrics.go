package outbox

import (
	"context"
	"fmt"
	"time"

	"gochen/storage/database"
)

// OutboxMetrics Outbox 监控指标
//
// 提供 Outbox 的实时统计信息，用于监控和告警。
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

// IMetricsCollector 指标采集器接口
type IMetricsCollector interface {
	// Collect 采集当前指标
	Collect(ctx context.Context) (*OutboxMetrics, error)

	// GetHealthStatus 获取健康状态
	//
	// 返回健康状态和描述信息。
	GetHealthStatus(ctx context.Context) (HealthStatus, string, error)
}

// HealthStatus 健康状态
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"   // 健康
	HealthStatusDegraded  HealthStatus = "degraded"  // 降级
	HealthStatusUnhealthy HealthStatus = "unhealthy" // 不健康
)

// MetricsCollector 指标采集器实现
type MetricsCollector struct {
	db database.IDatabase

	// 健康阈值配置
	maxPendingCount int64         // 待处理记录数阈值
	maxFailedCount  int64         // 失败记录数阈值
	maxDLQCount     int64         // DLQ 记录数阈值
	maxPendingAge   time.Duration // 最老待处理记录年龄阈值
}

// NewMetricsCollector 创建指标采集器
//
// 使用默认健康阈值：
//   - maxPendingCount: 10000
//   - maxFailedCount: 1000
//   - maxDLQCount: 100
//   - maxPendingAge: 1 小时
func NewMetricsCollector(db database.IDatabase) IMetricsCollector {
	return &MetricsCollector{
		db:              db,
		maxPendingCount: 10000,
		maxFailedCount:  1000,
		maxDLQCount:     100,
		maxPendingAge:   1 * time.Hour,
	}
}

// NewMetricsCollectorWithThresholds 创建指标采集器（自定义阈值）
func NewMetricsCollectorWithThresholds(
	db database.IDatabase,
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

// Collect 采集当前指标
func (c *MetricsCollector) Collect(ctx context.Context) (*OutboxMetrics, error) {
	metrics := &OutboxMetrics{
		CollectedAt: time.Now(),
	}

	// 1. 采集各状态的记录数
	if err := c.collectStatusCounts(ctx, metrics); err != nil {
		return nil, fmt.Errorf("collect status counts: %w", err)
	}

	// 2. 采集 DLQ 记录数
	if err := c.collectDLQCount(ctx, metrics); err != nil {
		// DLQ 可能不存在，忽略错误
		metrics.DLQCount = 0
	}

	// 3. 采集重试统计
	if err := c.collectRetryStats(ctx, metrics); err != nil {
		return nil, fmt.Errorf("collect retry stats: %w", err)
	}

	// 4. 采集时间统计
	if err := c.collectTimeStats(ctx, metrics); err != nil {
		return nil, fmt.Errorf("collect time stats: %w", err)
	}

	return metrics, nil
}

// collectStatusCounts 采集各状态的记录数
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

// collectDLQCount 采集 DLQ 记录数
func (c *MetricsCollector) collectDLQCount(ctx context.Context, metrics *OutboxMetrics) error {
	query := `SELECT COUNT(*) FROM event_outbox_dlq`

	err := c.db.QueryRow(ctx, query).Scan(&metrics.DLQCount)
	if err != nil {
		return err
	}

	return nil
}

// collectRetryStats 采集重试统计
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

// collectTimeStats 采集时间统计
func (c *MetricsCollector) collectTimeStats(ctx context.Context, metrics *OutboxMetrics) error {
	// 最老待处理记录的年龄
	var oldestCreatedAt *time.Time
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
	query = `
		SELECT AVG(TIMESTAMPDIFF(SECOND, created_at, published_at))
		FROM event_outbox
		WHERE status = 'published' AND published_at IS NOT NULL
		LIMIT 1000
	`

	var avgDelaySeconds float64
	err = c.db.QueryRow(ctx, query).Scan(&avgDelaySeconds)
	if err != nil {
		// 可能没有已发布记录
		return nil
	}

	metrics.AvgPublishDelay = time.Duration(avgDelaySeconds) * time.Second

	return nil
}

// GetHealthStatus 获取健康状态
func (c *MetricsCollector) GetHealthStatus(ctx context.Context) (HealthStatus, string, error) {
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

// MetricsSnapshot 指标快照
//
// 用于导出指标到外部监控系统（如 Prometheus）。
type MetricsSnapshot struct {
	Timestamp time.Time         `json:"timestamp"`
	Metrics   *OutboxMetrics    `json:"metrics"`
	Health    HealthStatus      `json:"health"`
	Issues    string            `json:"issues,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// TakeSnapshot 获取指标快照
func (c *MetricsCollector) TakeSnapshot(ctx context.Context, labels map[string]string) (*MetricsSnapshot, error) {
	metrics, err := c.Collect(ctx)
	if err != nil {
		return nil, err
	}

	health, issues, err := c.GetHealthStatus(ctx)
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
