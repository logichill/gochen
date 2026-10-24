package governance

import (
	"context"
	"time"

	"gochen/auth/scoped"
)

// --- Metrics 层：指标上报，永不影响授权 ---

type metricsAuthorizer struct {
	inner scoped.IAuthorizer
	cfg   *config
}

func (a *metricsAuthorizer) Authorize(ctx context.Context, action string, targets ...any) (scoped.Decision, error) {
	start := a.cfg.clock.Now()
	decision, err := a.inner.Authorize(ctx, action, targets...)
	a.record(ctx, action, decision, a.cfg.clock.Now().Sub(start))
	return decision, err
}

func (a *metricsAuthorizer) Require(ctx context.Context, action string, targets ...any) error {
	return requireVia(ctx, a, action, targets...)
}

// record 上报指标；实现 panic 也不得影响授权结果。
func (a *metricsAuthorizer) record(ctx context.Context, action string, decision scoped.Decision, latency time.Duration) {
	defer func() { _ = recover() }()
	a.cfg.metrics.RecordDecision(ctx, action, string(decision.Effect), latency)
}
