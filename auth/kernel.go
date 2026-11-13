package auth

import (
	"context"
	"gochen/clock"
	"gochen/domain/meta"
	"gochen/domain/spec"
	"gochen/errors"
	"gochen/gen"
	"gochen/internal/depcheck"
	"strings"
)

// GovernanceOptions 配置完整授权执行的版本留痕、审计与指标。
type GovernanceOptions struct {
	Snapshot        IPolicySnapshot
	Audit           IAuthzAuditStore
	Metrics         IAuthzMetrics
	StrictAudit     bool
	LenientSnapshot bool
}

// Options 配置完整授权器；零值时钟和生成器采用默认实现。
type Options struct {
	Policy      IPolicy
	Governance  GovernanceOptions
	Clock       clock.IClock
	IDGenerator gen.IGenerator[string]
	Metadata    *meta.Metadata
}

type config struct {
	snapshot        IPolicySnapshot
	auditStore      IAuthzAuditStore
	metrics         IAuthzMetrics
	clock           clock.IClock
	idGenerator     gen.IGenerator[string]
	strictAudit     bool
	lenientSnapshot bool
}

// Authorizer 执行策略、结果校验与固定顺序的治理。
type Authorizer struct {
	inner       IAuthorizer
	vocabulary  Vocabulary
	plan        *Plan
	fastDeny    func(context.Context, string) error
	factSources []IFactSource
}

// New 冻结策略词汇和配置，构造唯一的完整授权执行链。
// 返回的授权器处于未绑定编译计划状态；资源执行入口登记通过 Compile 装配。
func New(options Options) (*Authorizer, error) {
	if depcheck.IsNil(options.Policy) {
		return nil, errors.NewCode(errors.InvalidInput, "policy cannot be nil")
	}
	g := options.Governance
	for _, dependency := range []struct {
		name  string
		value any
	}{{"snapshot", g.Snapshot}, {"audit", g.Audit}, {"metrics", g.Metrics}, {"clock", options.Clock}, {"ID generator", options.IDGenerator}} {
		if dependency.value != nil && depcheck.IsNil(dependency.value) {
			return nil, errors.NewCode(errors.InvalidInput, "authorization dependency cannot be typed nil").WithContext("dependency", dependency.name)
		}
	}
	if g.StrictAudit && g.Audit == nil {
		return nil, errors.NewCode(errors.InvalidInput, "strict audit requires an audit store")
	}
	if g.LenientSnapshot && g.Snapshot == nil {
		return nil, errors.NewCode(errors.InvalidInput, "lenient snapshot requires a snapshot provider")
	}
	v := options.Policy.Vocabulary().Copy()
	sources := collectFactSources(options.Policy)
	byName := map[string]IFactSource{}
	for _, source := range sources {
		if depcheck.IsNil(source) || strings.TrimSpace(source.FactName()) == "" {
			return nil, errors.NewCode(errors.InvalidInput, "fact source cannot be nil or unnamed")
		}
		if previous, exists := byName[source.FactName()]; exists && previous != source {
			return nil, errors.NewCode(errors.InvalidInput, "conflicting fact sources").WithContext("fact", source.FactName())
		}
		byName[source.FactName()] = source
	}
	for _, ref := range v.Refs {
		if err := ref.Validate(); err != nil {
			return nil, err
		}
	}
	for _, dim := range v.Assignable {
		if _, err := meta.ParseDimension(string(dim)); err != nil {
			return nil, err
		}
	}
	if options.Metadata != nil {
		if err := v.Validate(options.Metadata); err != nil {
			return nil, err
		}
	}
	cfg := &config{snapshot: g.Snapshot, auditStore: g.Audit, metrics: g.Metrics, clock: options.Clock, idGenerator: options.IDGenerator, strictAudit: g.StrictAudit, lenientSnapshot: g.LenientSnapshot}
	if cfg.clock == nil {
		cfg.clock = clock.NewRealClock()
	}
	if cfg.idGenerator == nil {
		cfg.idGenerator = gen.NewUUIDGenerator()
	}
	var inner IAuthorizer = &policyAuthorizer{policy: options.Policy, vocabulary: v, metadata: options.Metadata}
	if cfg.snapshot != nil {
		inner = &snapshotAuthorizer{inner: inner, cfg: cfg}
	}
	if cfg.auditStore != nil {
		inner = &auditAuthorizer{inner: inner, cfg: cfg}
	}
	if cfg.metrics != nil {
		inner = &metricsAuthorizer{inner: inner, cfg: cfg}
	}
	return &Authorizer{inner: inner, vocabulary: v, factSources: append([]IFactSource(nil), sources...)}, nil
}

// Authorize 执行一次完整授权，并在返回前关闭本次可信事实会话。
func (a *Authorizer) Authorize(ctx context.Context, req Request) (Decision, error) {
	decision, managed, err := a.AuthorizeRequest(ctx, req)
	managed.CloseFacts()
	return decision, err
}

// AuthorizeRequest 返回决策及受管理的冻结请求，供显式多资源编排复用可信投影。
// 调用方必须在所属执行结束时调用 CloseFacts；普通应用使用 Authorize 自动关闭。
func (a *Authorizer) AuthorizeRequest(ctx context.Context, req Request) (Decision, Request, error) {
	frozen, err := a.prepareRequest(ctx, req)
	if err != nil {
		return Decision{}, Request{}, err
	}
	decision, err := a.inner.Authorize(ctx, frozen)
	return decision, frozen, err
}

// Vocabulary 返回构造期词汇的独立副本。
func (a *Authorizer) Vocabulary() Vocabulary { return a.vocabulary.Copy() }

// UsesAllowAuthorizer 返回显式放行标记。
func (a *Authorizer) UsesAllowAuthorizer() bool { return IsAllow(a.inner) }

// FastDeny 检查可安全提前拒绝的动作，沿用编译计划的检查器；完整授权继续由执行入口调用。
func (a *Authorizer) FastDeny(ctx context.Context, action string) error {
	if a.fastDeny == nil {
		return nil
	}
	return a.fastDeny(ctx, action)
}

// CheckFastDeny 调用授权器提供的提前拒绝能力。
func CheckFastDeny(ctx context.Context, authorizer IAuthorizer, action string) error {
	if provider, ok := authorizer.(interface {
		FastDeny(context.Context, string) error
	}); ok {
		return provider.FastDeny(ctx, action)
	}
	return nil
}

type policyAuthorizer struct {
	policy     IPolicy
	vocabulary Vocabulary
	metadata   *meta.Metadata
}

func (a *policyAuthorizer) Authorize(ctx context.Context, req Request) (Decision, error) {
	frozen, err := req.Copy()
	if err != nil {
		return Decision{}, err
	}
	d, err := a.policy.Evaluate(ctx, frozen)
	d.MatchedRules = appendUniqueRules(nil, d.MatchedRules...)
	if err != nil || !d.Allowed {
		d.Allowed = false
		d.Spec = spec.Spec{}
		d.Assign = nil
		return d, err
	}
	if err := validateDecisionShape(d); err != nil {
		return Decision{Reason: d.Reason, MatchedRules: d.MatchedRules}, err
	}
	if a.metadata != nil {
		if err := d.ValidateAgainst(a.vocabulary, a.metadata); err != nil {
			return Decision{MatchedRules: d.MatchedRules}, err
		}
	} else {
		for _, ref := range d.Spec.Refs() {
			found := false
			for _, allowed := range a.vocabulary.Refs {
				found = found || ref == allowed
			}
			if !found {
				return Decision{MatchedRules: d.MatchedRules}, errors.NewCode(errors.Internal, "decision reference is outside vocabulary").WithContext("reference", ref.String())
			}
		}
		for _, assignment := range d.Assign {
			found := false
			for _, dim := range a.vocabulary.Assignable {
				found = found || dim == assignment.Dimension
			}
			if !found {
				return Decision{MatchedRules: d.MatchedRules}, errors.NewCode(errors.Internal, "decision assignment is outside vocabulary").WithContext("dimension", string(assignment.Dimension))
			}
		}
	}
	d.Assign, err = meta.Clone(d.Assign)
	if err != nil {
		return Decision{MatchedRules: d.MatchedRules}, err
	}
	return d, nil
}

func (a *policyAuthorizer) Vocabulary() Vocabulary { return a.vocabulary.Copy() }
func (a *policyAuthorizer) UsesAllowAuthorizer() bool {
	marker, ok := a.policy.(interface{ UsesAllowAuthorizer() bool })
	return ok && marker.UsesAllowAuthorizer()
}

func decisionEffect(d Decision) string {
	if d.Allowed {
		return "allow"
	}
	return "deny"
}
