package governance

import (
	"context"
	"time"
)

// IAuthzMetrics 上报授权决策指标。
type IAuthzMetrics interface {
	RecordDecision(ctx context.Context, action string, effect string, latency time.Duration)
}

// AuthzMetricsFunc 允许用函数直接实现 IAuthzMetrics。
type AuthzMetricsFunc func(ctx context.Context, action string, effect string, latency time.Duration)

// RecordDecision 上报一次决策指标。
func (f AuthzMetricsFunc) RecordDecision(ctx context.Context, action string, effect string, latency time.Duration) {
	if f == nil {
		return
	}
	f(ctx, action, effect, latency)
}
