package monitoring

import (
	"context"
	"encoding/json"

	"gochen/errors"
	"gochen/eventing/monitoring"
	"gochen/eventing/outbox"
)

type provider struct {
	collector outbox.IMetricsCollector
}

// NewProvider 创建提供者。
func NewProvider(collector outbox.IMetricsCollector) (monitoring.IOutboxMetricsProvider, error) {
	if collector == nil {
		return nil, errors.NewCode(errors.InvalidInput, "collector cannot be nil")
	}
	return &provider{collector: collector}, nil
}

func (p *provider) Health(ctx context.Context) (monitoring.HealthStatus, string, error) {
	status, msg, err := p.collector.HealthStatus(ctx)
	return mapHealthStatus(status), msg, err
}

func (p *provider) Snapshot(ctx context.Context) (*monitoring.OutboxSnapshot, error) {
	metrics, err := p.collector.Collect(ctx)
	if err != nil {
		return nil, err
	}
	health, issues, err := p.collector.HealthStatus(ctx)
	if err != nil {
		return nil, err
	}
	return &monitoring.OutboxSnapshot{
		Metrics: metricsToMap(metrics),
		Health:  mapHealthStatus(health),
		Issues:  issues,
	}, nil
}

func metricsToMap(m *outbox.OutboxMetrics) map[string]any {
	if m == nil {
		return nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return map[string]any{"_error": err.Error()}
	}
	var v map[string]any
	if err := json.Unmarshal(b, &v); err != nil {
		return map[string]any{"_error": err.Error()}
	}
	return v
}

// mapHealthStatus 整理健康检查状态。
func mapHealthStatus(s outbox.HealthStatus) monitoring.HealthStatus {
	switch s {
	case outbox.HealthStatusHealthy:
		return monitoring.HealthStatusHealthy
	case outbox.HealthStatusDegraded:
		return monitoring.HealthStatusDegraded
	case outbox.HealthStatusUnhealthy:
		return monitoring.HealthStatusUnhealthy
	default:
		return monitoring.HealthStatusUnhealthy
	}
}
