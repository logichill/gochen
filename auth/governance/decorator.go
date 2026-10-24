package governance

import (
	"context"

	"gochen/auth/scoped"
	"gochen/clock"
	"gochen/errors"
	"gochen/gen"
)

// Wrap 以治理外壳装饰 Authorizer。
//
// 固化调用链（外 → 内）：Metrics(Log(Snapshot(base)))。
//   - Snapshot 最内层：先固定策略版本，后续留痕与指标才有一致的版本口径；
//   - Log 居中：记录的是已带快照版本的最终决策；
//   - Metrics 最外层：统计包含全部治理开销的真实延迟。
//
// 包装顺序由本函数固化，业务层不得自行拼接（§4.3）。
func Wrap(base scoped.IAuthorizer, opts ...Option) (scoped.IAuthorizer, error) {
	if base == nil {
		return nil, errors.NewCode(errors.InvalidInput, "base authorizer cannot be nil")
	}
	cfg := &config{clock: clock.NewRealClock(), idGenerator: gen.NewUUIDGenerator()}
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}
	if cfg.clock == nil {
		return nil, errors.NewCode(errors.InvalidInput, "governance clock cannot be nil")
	}
	if cfg.idGenerator == nil {
		return nil, errors.NewCode(errors.InvalidInput, "governance ID generator cannot be nil")
	}
	if cfg.snapshot == nil && cfg.auditStore == nil && cfg.metrics == nil {
		return nil, errors.NewCode(errors.InvalidInput,
			"governance requires at least one of snapshot, audit, or metrics")
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

// requireVia 让各层 Require 复用自身 Authorize，保证治理逻辑对两个入口都生效。
// 无具体目标（动作级鉴权）时按 IsActionAllowed 判定，避免空资源清单误杀动作级授权。
func requireVia(ctx context.Context, authorizer scoped.IAuthorizer, action string, targets ...any) error {
	decision, err := authorizer.Authorize(ctx, action, targets...)
	if err != nil {
		return err
	}
	if !decision.AllowedFor(len(targets)) {
		return errors.NewCode(errors.Forbidden, "authorization denied").
			WithContext("action", action).
			WithContext("reason", decision.ReasonCode)
	}
	return nil
}
