package governance

import (
	"context"
	"time"

	"gochen/auth/scoped"

	"gochen/clock"
	"gochen/errors"
	"gochen/gen"
)

// Option 配置治理外壳。
type Option func(*config)

type config struct {
	snapshot    IPolicySnapshot
	auditStore  IAuthzAuditStore
	metrics     IAuthzMetrics
	clock       clock.IClock
	idGenerator gen.IGenerator[string]

	// strictAudit 为 true 时审计写入失败会阻塞授权（默认 false）。
	strictAudit bool
	// lenientSnapshot 为 true 时快照解析失败不阻塞授权（默认 false）。
	lenientSnapshot bool
}

// WithSnapshot 启用策略快照一致性。
//
// 失败策略：默认 **fail-closed**——快照获取失败直接拒绝请求。
// 可用 WithLenientSnapshot 覆盖为宽松模式。
func WithSnapshot(snapshot IPolicySnapshot) Option {
	return func(c *config) { c.snapshot = snapshot }
}

// WithAudit 启用决策审计留痕。
//
// 失败策略：默认 **fail-open**——审计写入失败不阻塞授权。
// 可靠重试与告警由 store 或外部队列负责；可用 WithStrictAudit 改为阻塞授权。
func WithAudit(store IAuthzAuditStore) Option {
	return func(c *config) { c.auditStore = store }
}

// WithMetrics 启用决策指标上报。
//
// 失败策略：静默降级，永不影响授权。
func WithMetrics(metrics IAuthzMetrics) Option {
	return func(c *config) { c.metrics = metrics }
}

// WithStrictAudit 把审计失败改为阻塞授权（合规严格模式）。
func WithStrictAudit() Option {
	return func(c *config) { c.strictAudit = true }
}

// WithLenientSnapshot 把快照解析失败改为不阻塞授权。
//
// 谨慎使用：这会让判定退回到无快照保证的实时评估。
func WithLenientSnapshot() Option {
	return func(c *config) { c.lenientSnapshot = true }
}

// WithClock 注入时钟（便于测试确定性）。
func WithClock(c clock.IClock) Option {
	return func(cfg *config) { cfg.clock = c }
}

// WithIDGenerator 注入决策 ID 生成器。
func WithIDGenerator(g gen.IGenerator[string]) Option {
	return func(c *config) { c.idGenerator = g }
}

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

// --- Snapshot 层：固定策略版本 ---

type snapshotAuthorizer struct {
	inner scoped.IAuthorizer
	cfg   *config
}

func (a *snapshotAuthorizer) Authorize(ctx context.Context, action string, targets ...any) (scoped.Decision, error) {
	snapshot, err := a.cfg.snapshot.ResolveSnapshot(ctx)
	if err != nil {
		if !a.cfg.lenientSnapshot {
			// Fail-Closed：拿不到策略快照说明无法保证判定依据的一致性。
			return scoped.Deny("snapshot_unavailable"), errors.Wrap(err, errors.Forbidden,
				"policy snapshot is unavailable; denying request")
		}
		// 宽松模式：继续判定，但不带快照版本。
		return a.inner.Authorize(ctx, action, targets...)
	}
	decision, err := a.inner.Authorize(ctx, action, targets...)
	if err != nil {
		return decision, err
	}
	decision.SnapshotVersion = NormalizeSnapshot(snapshot).Version
	return decision, nil
}

func (a *snapshotAuthorizer) Require(ctx context.Context, action string, targets ...any) error {
	return requireVia(ctx, a, action, targets...)
}

// --- Audit 层：决策留痕 ---

type auditAuthorizer struct {
	inner scoped.IAuthorizer
	cfg   *config
}

func (a *auditAuthorizer) Authorize(ctx context.Context, action string, targets ...any) (scoped.Decision, error) {
	start := a.cfg.clock.Now()
	decision, err := a.inner.Authorize(ctx, action, targets...)
	if decision.ID == "" {
		id, genErr := a.cfg.idGenerator.Next()
		switch {
		case genErr != nil && a.cfg.strictAudit:
			// 严格模式与审计写入失败同口径：留痕要素缺失即拒绝。
			return scoped.Deny("audit_unavailable"), errors.Wrap(genErr, errors.Forbidden,
				"failed to assign decision ID; denying request")
		case genErr == nil:
			decision.ID = id
		}
	}

	entry := AuthzLogEntry{
		ID:              decision.ID,
		Type:            AuthzLogEntryTypeDecision,
		Timestamp:       start,
		DecisionID:      decision.ID,
		Action:          action,
		Effect:          string(decision.Effect),
		ReasonCode:      decision.ReasonCode,
		SnapshotVersion: decision.SnapshotVersion,
		LatencyMs:       a.cfg.clock.Now().Sub(start).Milliseconds(),
		Resources:       LoggedResourcesOf(decision),
		MatchedRules:    decision.MatchedRules,
	}
	if decision.SnapshotVersion != "" {
		if diag, ok := a.cfg.snapshot.(snapshotDiagnostics); ok {
			entry.CacheHit = diag.CacheHit()
			entry.SnapshotKey = diag.SnapshotKey()
		}
	}
	// Wrap 在交付前统一脱敏：任何 IAuthzAuditStore（包括自定义实现）收到的条目
	// 都必须已剔除横向关联标识（TenantID / OwnerID），不依赖各家 store 自觉。
	// 需要保留完整字段的合规场景经 WithResourceLogSanitizer 注入；
	// store 侧可保留同构脱敏作为自身对直连调用方的输入边界（幂等）。
	entry = SanitizeAuthzLogEntry(entry)
	entry.Resources = SanitizeLoggedResources(ctx, entry.Resources)
	if auditErr := a.cfg.auditStore.SaveAuthzLogEntry(ctx, entry); auditErr != nil {
		if a.cfg.strictAudit {
			// 严格模式：留痕失败即拒绝，保证"无审计不放行"。
			return scoped.Deny("audit_unavailable"), errors.Wrap(auditErr, errors.Forbidden,
				"authorization audit store is unavailable; denying request")
		}
		// 默认 fail-open：审计不可用不阻塞授权。
	}
	return decision, err
}

func (a *auditAuthorizer) Require(ctx context.Context, action string, targets ...any) error {
	return requireVia(ctx, a, action, targets...)
}

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

// requireVia 让各层 Require 复用自身 Authorize，保证治理逻辑对两个入口都生效。
func requireVia(ctx context.Context, authorizer scoped.IAuthorizer, action string, targets ...any) error {
	decision, err := authorizer.Authorize(ctx, action, targets...)
	if err != nil {
		return err
	}
	if !decision.IsAllowed() {
		return errors.NewCode(errors.Forbidden, "authorization denied").
			WithContext("action", action).
			WithContext("reason", decision.ReasonCode)
	}
	return nil
}
