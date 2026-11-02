package governance

import (
	"gochen/auth"
	"gochen/clock"
	"gochen/errors"
	"gochen/gen"
	"gochen/internal/depcheck"
)

// Wrap 以治理外壳装饰 Authorizer。
//
// 固化调用链（外 → 内）：Metrics(Log(Snapshot(base)))。
//   - Snapshot 最内层：先解析版本，再执行 base；实际策略版本由 base 负责绑定；
//   - Log 居中：记录最终决策与解析器提供的版本；
//   - Metrics 最外层：统计包含全部治理开销的真实延迟。
//
// 包装顺序由本函数固化，业务层不得自行拼接（§4.3）。
func Wrap(base auth.IAuthorizer, opts ...Option) (auth.IAuthorizer, error) {
	if depcheck.IsNil(base) {
		return nil, errors.NewCode(errors.InvalidInput, "base authorizer cannot be nil")
	}
	cfg := &config{clock: clock.NewRealClock(), idGenerator: gen.NewUUIDGenerator()}
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}
	if depcheck.IsNil(cfg.clock) {
		return nil, errors.NewCode(errors.InvalidInput, "governance clock cannot be nil")
	}
	if depcheck.IsNil(cfg.idGenerator) {
		return nil, errors.NewCode(errors.InvalidInput, "governance ID generator cannot be nil")
	}
	if cfg.snapshot == nil && cfg.auditStore == nil && cfg.metrics == nil {
		return nil, errors.NewCode(errors.InvalidInput,
			"governance requires at least one of snapshot, audit, or metrics")
	}
	if cfg.snapshot != nil && depcheck.IsNil(cfg.snapshot) {
		return nil, errors.NewCode(errors.InvalidInput, "governance snapshot cannot be typed nil")
	}
	if cfg.auditStore != nil && depcheck.IsNil(cfg.auditStore) {
		return nil, errors.NewCode(errors.InvalidInput, "governance audit store cannot be typed nil")
	}
	if cfg.metrics != nil && depcheck.IsNil(cfg.metrics) {
		return nil, errors.NewCode(errors.InvalidInput, "governance metrics cannot be typed nil")
	}

	authorizer := base
	if cfg.snapshot != nil {
		authorizer = &snapshotAuthorizer{inner: authorizer, cfg: cfg}
	}
	if cfg.auditStore != nil {
		authorizer = &auditAuthorizer{inner: authorizer, cfg: cfg}
	}
	if cfg.metrics != nil {
		authorizer = &metricsAuthorizer{inner: authorizer, cfg: cfg}
	}
	return authorizer, nil
}

// decisionEffect 将统一判定转换为日志和指标使用的事实值。
func decisionEffect(d auth.Decision) string {
	if d.Allowed {
		return "allow"
	}
	return "deny"
}
