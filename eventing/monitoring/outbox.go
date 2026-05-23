package monitoring

import (
	"context"

	"gochen/errors"
)

// IOutboxMetricsProvider 定义监控模块读取 Outbox 指标与健康状态所需的最小能力。
type IOutboxMetricsProvider interface {
	Snapshot(ctx context.Context) (*OutboxSnapshot, error)
	Health(ctx context.Context) (HealthStatus, string, error)
}

// OutboxSnapshot 表示可直接序列化输出的 Outbox 观测快照。
type OutboxSnapshot struct {
	// Metrics 是可序列化的结构化输出（避免绑定到 outbox 包的具体类型）。
	// 推荐实现：使用 json marshal/unmarshal 将 outbox.OutboxMetrics 转换为 map。
	Metrics map[string]any `json:"metrics,omitempty"`
	Health  HealthStatus   `json:"health,omitempty"`
	Issues  string         `json:"issues,omitempty"`
	Error   string         `json:"error,omitempty"`
}

// OutboxHealthCheck 把 Outbox provider 适配为健康检查函数。
func OutboxHealthCheck(p IOutboxMetricsProvider) (Check, error) {
	if p == nil {
		return nil, errors.NewCode(errors.InvalidInput, "outbox provider cannot be nil")
	}
	return func(ctx context.Context) (HealthStatus, string, error) { return p.Health(ctx) }, nil
}
