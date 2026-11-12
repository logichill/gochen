package auth

import (
	"context"
	"time"
)

// --- Metrics 层：指标上报，永不影响授权 ---

type metricsAuthorizer struct {
	inner IAuthorizer
	cfg   *config
}

func (a *metricsAuthorizer) Authorize(ctx context.Context, req Request) (Decision, error) {
	start := a.cfg.clock.Now()
	decision, err := a.inner.Authorize(ctx, req)
	a.record(ctx, req.Permission, decision, a.cfg.clock.Now().Sub(start))
	return decision, err
}

// record 上报指标；实现 panic 也不得影响授权结果。
func (a *metricsAuthorizer) record(ctx context.Context, action string, decision Decision, latency time.Duration) {
	defer func() { _ = recover() }()
	a.cfg.metrics.RecordDecision(ctx, action, decisionEffect(decision), latency)
}

func (a *metricsAuthorizer) Vocabulary() Vocabulary { return a.inner.Vocabulary().Copy() }

func (a *metricsAuthorizer) UsesAllowAuthorizer() bool { return IsAllow(a.inner) }
