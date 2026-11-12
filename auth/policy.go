package auth

import (
	"context"
	"reflect"
	"sort"

	"gochen/domain/meta"
	"gochen/domain/spec"
	"gochen/errors"
	"gochen/internal/depcheck"
)

// IPolicy 判定授权规则；完整执行与治理由 Authorizer 承担。
type IPolicy interface {
	Evaluate(context.Context, Request) (Decision, error)
	Vocabulary() Vocabulary
}

// PolicyFunc 将无字段引用的判定函数接入策略。
type PolicyFunc func(context.Context, Request) (Decision, error)

// Evaluate 执行判定函数。
func (f PolicyFunc) Evaluate(ctx context.Context, req Request) (Decision, error) { return f(ctx, req) }

// Vocabulary 返回函数策略的空词汇。
func (PolicyFunc) Vocabulary() Vocabulary { return Vocabulary{} }

// ISpecResolver 根据可信资料解析范围条件与权威赋值。
type ISpecResolver interface {
	ResolveSpec(context.Context, Request) (spec.Spec, []Assignment, error)
	Vocabulary() Vocabulary
}

// SpecResolverFunc 将无可引用字段的范围函数接入解析端口。
type SpecResolverFunc func(context.Context, Request) (spec.Spec, []Assignment, error)

// ResolveSpec 执行范围函数。
func (f SpecResolverFunc) ResolveSpec(ctx context.Context, req Request) (spec.Spec, []Assignment, error) {
	return f(ctx, req)
}

// Vocabulary 返回函数解析器的空词汇。
func (SpecResolverFunc) Vocabulary() Vocabulary { return Vocabulary{} }

// PermissionOption 配置权限策略。
type PermissionOption func(*permissionPolicy)

type permissionPolicy struct {
	checker           IActionChecker
	scope             ISpecResolver
	vocabulary        Vocabulary
	denyUnauthorized  bool
	reason            string
	systemConstraints bool
	scopeEnabled      bool
}

// WithScope 显式启用范围解析；nil 解析器在构造时拒绝。
func WithScope(resolver ISpecResolver) PermissionOption {
	return func(p *permissionPolicy) { p.scope = resolver; p.scopeEnabled = true }
}

// WithPermissionDenial 冻结权限拒绝原因和缺身份转拒绝行为。
func WithPermissionDenial(reason string, unauthorizedAsDenial bool) PermissionOption {
	return func(p *permissionPolicy) { p.reason = reason; p.denyUnauthorized = unauthorizedAsDenial }
}

// WithSystemConstraints 保留 System 的范围约束，动作权限仍由系统身份放行。
func WithSystemConstraints() PermissionOption {
	return func(p *permissionPolicy) { p.systemConstraints = true }
}

// NewPermissionPolicy 创建先检查权限、再按需解析范围的策略。
func NewPermissionPolicy(checker IActionChecker, options ...PermissionOption) (IPolicy, error) {
	if depcheck.IsNil(checker) {
		return nil, errors.NewCode(errors.InvalidInput, "action checker cannot be nil")
	}
	p := &permissionPolicy{checker: checker, reason: "forbidden"}
	for _, option := range options {
		if option != nil {
			option(p)
		}
	}
	if p.scopeEnabled {
		if depcheck.IsNil(p.scope) {
			return nil, errors.NewCode(errors.InvalidInput, "spec resolver cannot be nil")
		}
		p.vocabulary = p.scope.Vocabulary().Copy()
	}
	return p, nil
}

func (p *permissionPolicy) Evaluate(ctx context.Context, req Request) (Decision, error) {
	principal, system := PrincipalFromContext(ctx)
	system = system && principal.System
	if system && !p.systemConstraints {
		return Decision{Allowed: true, Spec: spec.All()}, nil
	}
	if req.Permission == "" {
		return Decision{}, errors.NewCode(errors.InvalidInput, "request permission is required")
	}
	if !system {
		if err := RequireAction(ctx, p.checker, req.Permission); err != nil {
			if errors.Code(err) == errors.Forbidden || p.denyUnauthorized && errors.Code(err) == errors.Unauthorized {
				return Decision{Reason: p.reason}, nil
			}
			return Decision{}, err
		}
	}
	if !p.scopeEnabled {
		return Decision{Allowed: true, Spec: spec.All()}, nil
	}
	s, assignments, err := p.scope.ResolveSpec(ctx, req)
	if err != nil {
		return Decision{}, err
	}
	if err := validateDecisionShape(Decision{Allowed: true, Spec: s, Assign: assignments}); err != nil {
		return Decision{}, err
	}
	return Decision{Allowed: true, Spec: s, Assign: assignments}, nil
}

func (p *permissionPolicy) Vocabulary() Vocabulary     { return p.vocabulary.Copy() }
func (p *permissionPolicy) FactSources() []IFactSource { return collectFactSources(p.scope) }

type allowPolicy struct{}

// AllowPolicy 返回显式放行策略；仓储隔离继续按资源契约执行。
func AllowPolicy() IPolicy { return allowPolicy{} }

func (allowPolicy) Evaluate(context.Context, Request) (Decision, error) {
	return Decision{Allowed: true, Spec: spec.All()}, nil
}
func (allowPolicy) Vocabulary() Vocabulary    { return Vocabulary{} }
func (allowPolicy) UsesAllowAuthorizer() bool { return true }

type allPolicy struct {
	policies   []IPolicy
	vocabulary Vocabulary
}

// AllPolicies 按顺序执行交集，拒绝及错误短路，合并已执行规则依据。
func AllPolicies(policies ...IPolicy) (IPolicy, error) {
	if len(policies) == 0 {
		return nil, errors.NewCode(errors.InvalidInput, "at least one policy is required")
	}
	p := &allPolicy{policies: append([]IPolicy(nil), policies...)}
	for _, policy := range p.policies {
		if depcheck.IsNil(policy) {
			return nil, errors.NewCode(errors.InvalidInput, "policy cannot be nil")
		}
		p.vocabulary = mergeVocabulary(p.vocabulary, policy.Vocabulary())
	}
	return p, nil
}

func (p *allPolicy) Evaluate(ctx context.Context, req Request) (Decision, error) {
	var conditions []spec.Spec
	var matched []string
	assignments := map[meta.Dimension]any{}
	for _, policy := range p.policies {
		frozen, err := req.Copy()
		if err != nil {
			return Decision{MatchedRules: matched}, err
		}
		d, err := policy.Evaluate(ctx, frozen)
		matched = appendUniqueRules(matched, d.MatchedRules...)
		if err != nil {
			return Decision{Reason: d.Reason, MatchedRules: matched}, err
		}
		if !d.Allowed {
			if d.Reason == "" {
				d.Reason = "forbidden"
			}
			return Decision{Reason: d.Reason, MatchedRules: matched}, nil
		}
		if err := validateDecisionShape(d); err != nil {
			return Decision{MatchedRules: matched}, err
		}
		d.Assign, err = meta.Clone(d.Assign)
		if err != nil {
			return Decision{MatchedRules: matched}, err
		}
		conditions = append(conditions, d.Spec)
		for _, assignment := range d.Assign {
			if existing, ok := assignments[assignment.Dimension]; ok && !reflect.DeepEqual(existing, assignment.Value) {
				return Decision{Reason: "assignment_conflict", MatchedRules: matched}, nil
			}
			assignments[assignment.Dimension] = assignment.Value
		}
	}
	d := Decision{Allowed: true, Spec: spec.And(conditions...), MatchedRules: matched}
	for dim, value := range assignments {
		d.Assign = append(d.Assign, Assignment{Dimension: dim, Value: value})
	}
	sort.Slice(d.Assign, func(i, j int) bool { return d.Assign[i].Dimension < d.Assign[j].Dimension })
	return d, nil
}

func (p *allPolicy) Vocabulary() Vocabulary { return p.vocabulary.Copy() }
func (p *allPolicy) FactSources() []IFactSource {
	var sources []IFactSource
	for _, policy := range p.policies {
		sources = append(sources, collectFactSources(policy)...)
	}
	return sources
}
func (p *allPolicy) UsesAllowAuthorizer() bool {
	for _, policy := range p.policies {
		marker, ok := policy.(interface{ UsesAllowAuthorizer() bool })
		if !ok || !marker.UsesAllowAuthorizer() {
			return false
		}
	}
	return true
}

func appendUniqueRules(dst []string, rules ...string) []string {
	seen := make(map[string]bool, len(dst)+len(rules))
	for _, rule := range dst {
		seen[rule] = true
	}
	for _, rule := range rules {
		if !seen[rule] {
			dst = append(dst, rule)
			seen[rule] = true
		}
	}
	return dst
}

func validateDecisionShape(d Decision) error {
	if !d.Allowed {
		return nil
	}
	if d.Spec.Err() != nil {
		return errors.Wrap(d.Spec.Err(), errors.Internal, "policy returned invalid spec")
	}
	if d.Spec.HasNamed() {
		return errors.NewCode(errors.Internal, "authorization spec cannot contain named conditions")
	}
	seen := map[meta.Dimension]bool{}
	for _, assignment := range d.Assign {
		if err := assignment.Validate(); err != nil {
			return errors.Wrap(err, errors.Internal, "policy returned invalid assignment")
		}
		if seen[assignment.Dimension] {
			return errors.NewCode(errors.Internal, "duplicate decision assignment")
		}
		seen[assignment.Dimension] = true
	}
	return nil
}
