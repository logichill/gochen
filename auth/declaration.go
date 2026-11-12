package auth

import (
	"context"
	"reflect"
	"strings"

	"gochen/auth/action"
	"gochen/clock"
	"gochen/domain/meta"
	"gochen/domain/spec"
	"gochen/errors"
	"gochen/gen"
	"gochen/internal/depcheck"
)

// ResourceAccess 声明执行是否访问业务资源数据；零值表示未声明。
type ResourceAccess uint8

const (
	ResourceAccessUnspecified ResourceAccess = iota
	ResourceAccessNone
	ResourceAccessData
)

// SystemBehavior 声明系统身份对本条规则的执行方式。
type SystemBehavior uint8

const (
	// SystemBypassRule 沿用常规策略的系统身份放行行为。
	SystemBypassRule SystemBehavior = iota
	// SystemCheckConstraints 放行动作检查后继续执行范围及领域约束。
	SystemCheckConstraints
)

// ScopeRule 显式绑定范围解析能力；提供方声明类型化引用和赋值词汇。
type ScopeRule struct{ Resolver ISpecResolver }

// Rule 声明适用动作的权限、范围及有序扩展。空 Actions 表示默认规则。
type Rule struct {
	ID                   string
	Actions              []string
	Scope                *ScopeRule
	Constraints          []IPolicy
	OnPermissionDenied   IPolicy
	DeniedReason         string
	UnauthorizedAsDenial bool
	System               SystemBehavior
	AllowReason          string
}

// Declaration 引用动作权威描述并声明授权规则。
type Declaration struct {
	Descriptor     Descriptor
	ResourceAccess ResourceAccess
	Rules          []Rule
}

// CompileOptions 注入编译所需的可信能力与完整执行配置。
type CompileOptions struct {
	Checker     IActionChecker
	Metadata    *meta.Metadata
	Governance  GovernanceOptions
	Clock       clock.IClock
	IDGenerator gen.IGenerator[string]
}

// Plan 是构造期冻结的授权计划描述，供执行绑定和启动门禁核对。
type Plan struct {
	Descriptor          Descriptor
	ResourceAccess      ResourceAccess
	AllowReasons        map[string]string
	FastDenyPermissions map[string]string
	compiled            bool
	seal                *planSeal
}

type planSeal struct {
	descriptor          Descriptor
	resourceAccess      ResourceAccess
	allowReasons        map[string]string
	fastDenyPermissions map[string]string
}

// Validate 核对公开计划描述与编译期冻结记录，拒绝伪造和后续漂移。
func (p Plan) Validate() error {
	if !p.compiled || p.seal == nil || p.ResourceAccess != p.seal.resourceAccess || !reflect.DeepEqual(p.Descriptor, p.seal.descriptor) || !reflect.DeepEqual(p.AllowReasons, p.seal.allowReasons) || !reflect.DeepEqual(p.FastDenyPermissions, p.seal.fastDenyPermissions) {
		return errors.NewCode(errors.InvalidInput, "authorization plan differs from its compiled definition")
	}
	return p.Descriptor.Validate()
}

// Copy 返回计划描述的独立副本。
func (p Plan) Copy() Plan {
	p.Descriptor = p.Descriptor.Copy()
	reasons := make(map[string]string, len(p.AllowReasons))
	for action, reason := range p.AllowReasons {
		reasons[action] = reason
	}
	p.AllowReasons = reasons
	permissions := make(map[string]string, len(p.FastDenyPermissions))
	for action, permission := range p.FastDenyPermissions {
		permissions[action] = permission
	}
	p.FastDenyPermissions = permissions
	return p
}

type compiledPolicy struct {
	resource    string
	permissions map[string]string
	rules       map[string]*compiledRule
	vocabulary  Vocabulary
	allow       bool
}

type compiledRule struct {
	id          string
	permission  IPolicy
	constraints IPolicy
	alternative IPolicy
	system      SystemBehavior
	allow       bool
}

// Compile 校验强类型声明并构造完整授权器；资源字段由既有元数据解析。
func Compile(declaration Declaration, options CompileOptions) (*Authorizer, error) {
	if err := declaration.Descriptor.Validate(); err != nil {
		return nil, err
	}
	if len(declaration.Descriptor.Actions) == 0 {
		return nil, errors.NewCode(errors.InvalidInput, "declaration requires at least one action")
	}
	switch declaration.ResourceAccess {
	case ResourceAccessNone:
		if options.Metadata != nil {
			return nil, errors.NewCode(errors.InvalidInput, "action-only declaration cannot bind entity metadata")
		}
	case ResourceAccessData:
		if options.Metadata == nil {
			return nil, errors.NewCode(errors.InvalidInput, "resource declaration requires entity metadata")
		}
	default:
		return nil, errors.NewCode(errors.InvalidInput, "resource access must be declared")
	}
	descriptor := declaration.Descriptor.Copy()
	p := &compiledPolicy{resource: descriptor.Resource, permissions: map[string]string{}, rules: map[string]*compiledRule{}, allow: true}
	plan := Plan{Descriptor: descriptor, ResourceAccess: declaration.ResourceAccess, AllowReasons: map[string]string{}, FastDenyPermissions: map[string]string{}, compiled: true}
	var defaultRule Rule
	defaultSet := false
	byAction := map[string]Rule{}
	for _, rule := range declaration.Rules {
		if len(rule.Actions) == 0 {
			if defaultSet {
				return nil, errors.NewCode(errors.InvalidInput, "duplicate default authorization rule")
			}
			defaultRule, defaultSet = rule, true
		}
		for _, name := range rule.Actions {
			if _, ok := descriptor.Action(name); !ok {
				return nil, errors.NewCode(errors.InvalidInput, "authorization rule references undeclared action").WithContext("action", name)
			}
			if _, ok := byAction[name]; ok {
				return nil, errors.NewCode(errors.InvalidInput, "duplicate authorization action rule").WithContext("action", name)
			}
			byAction[name] = rule
		}
	}
	for _, declared := range descriptor.Actions {
		permission, ok := descriptor.PermissionFor(declared.Name)
		if !ok || !action.IsValidCode(permission) {
			return nil, errors.NewCode(errors.InvalidInput, "invalid declared permission").WithContext("action", declared.Name)
		}
		rule, ok := byAction[declared.Name]
		if !ok {
			rule = defaultRule
		}
		if rule.System > SystemCheckConstraints {
			return nil, errors.NewCode(errors.InvalidInput, "invalid system rule behavior")
		}
		if declaration.ResourceAccess == ResourceAccessNone && (rule.Scope != nil || len(rule.Constraints) > 0 || rule.OnPermissionDenied != nil) {
			return nil, errors.NewCode(errors.InvalidInput, "action-only declaration cannot require resource constraints")
		}
		compiled := &compiledRule{id: rule.ID, alternative: rule.OnPermissionDenied, system: rule.System, allow: strings.TrimSpace(rule.AllowReason) != ""}
		if compiled.alternative != nil && depcheck.IsNil(compiled.alternative) {
			return nil, errors.NewCode(errors.InvalidInput, "alternative policy cannot be typed nil")
		}
		if compiled.allow {
			if rule.Scope != nil || len(rule.Constraints) > 0 || compiled.alternative != nil {
				return nil, errors.NewCode(errors.InvalidInput, "explicit allow rule cannot include constraints or alternative")
			}
			compiled.permission = AllowPolicy()
			plan.AllowReasons[declared.Name] = strings.TrimSpace(rule.AllowReason)
		} else {
			var err error
			denial := rule.DeniedReason
			if denial == "" {
				denial = "forbidden"
			}
			compiled.permission, err = NewPermissionPolicy(options.Checker, WithPermissionDenial(denial, rule.UnauthorizedAsDenial))
			if err != nil {
				return nil, err
			}
			if compiled.alternative == nil {
				plan.FastDenyPermissions[declared.Name] = permission
			}
			p.allow = false
		}
		var constraints []IPolicy
		if rule.Scope != nil {
			if depcheck.IsNil(rule.Scope.Resolver) {
				return nil, errors.NewCode(errors.InvalidInput, "scope rule requires resolver")
			}
			constraints = append(constraints, &scopePolicy{resolver: rule.Scope.Resolver, vocabulary: rule.Scope.Resolver.Vocabulary().Copy()})
		}
		constraints = append(constraints, rule.Constraints...)
		if len(constraints) > 0 {
			var err error
			compiled.constraints, err = AllPolicies(constraints...)
			if err != nil {
				return nil, err
			}
		}
		for _, policy := range []IPolicy{compiled.permission, compiled.constraints, compiled.alternative} {
			if policy != nil {
				p.vocabulary = mergeVocabulary(p.vocabulary, policy.Vocabulary())
			}
		}
		p.rules[declared.Name], p.permissions[declared.Name] = compiled, permission
	}
	authorizer, err := New(Options{Policy: p, Metadata: options.Metadata, Governance: options.Governance, Clock: options.Clock, IDGenerator: options.IDGenerator})
	if err != nil {
		return nil, err
	}
	frozenPlan := plan.Copy()
	plan.seal = &planSeal{descriptor: frozenPlan.Descriptor, resourceAccess: frozenPlan.ResourceAccess, allowReasons: frozenPlan.AllowReasons, fastDenyPermissions: frozenPlan.FastDenyPermissions}
	authorizer.plan = &plan
	authorizer.fastDeny = func(ctx context.Context, name string) error {
		permission, safe := plan.FastDenyPermissions[name]
		if !safe {
			return nil
		}
		if principal, ok := PrincipalFromContext(ctx); ok && principal.System {
			return nil
		}
		decision, err := p.rules[name].permission.Evaluate(ctx, Request{Resource: p.resource, Action: name, Permission: permission})
		if err != nil {
			return err
		}
		if !decision.Allowed {
			return errors.NewCode(errors.Forbidden, "action permission denied").WithContext("reason", decision.Reason)
		}
		return nil
	}
	return authorizer, nil
}

func (p *compiledPolicy) Evaluate(ctx context.Context, req Request) (Decision, error) {
	if req.Resource != p.resource {
		return Decision{}, errors.NewCode(errors.InvalidInput, "request resource does not match declaration")
	}
	rule, ok := p.rules[req.Action]
	if !ok {
		return Decision{}, errors.NewCode(errors.Forbidden, "action is not declared").WithContext("action", req.Action)
	}
	permission := p.permissions[req.Action]
	if req.Permission != "" && req.Permission != permission {
		return Decision{}, errors.NewCode(errors.InvalidInput, "request permission does not match declared action")
	}
	req.Permission = permission
	d, err := rule.evaluate(ctx, req)
	if rule.id != "" {
		d.MatchedRules = appendUniqueRules([]string{rule.id}, d.MatchedRules...)
	}
	return d, err
}

func (r *compiledRule) evaluate(ctx context.Context, req Request) (Decision, error) {
	if principal, ok := PrincipalFromContext(ctx); ok && principal.System && r.system == SystemBypassRule {
		return Decision{Allowed: true, Spec: spec.All()}, nil
	}
	d, err := r.permission.Evaluate(ctx, req)
	if err != nil && errors.Code(err) != errors.Unauthorized && errors.Code(err) != errors.Forbidden {
		return d, err
	}
	if err != nil || !d.Allowed {
		if r.alternative != nil {
			alternative, alternativeErr := r.alternative.Evaluate(ctx, req)
			alternative.MatchedRules = appendUniqueRules(d.MatchedRules, alternative.MatchedRules...)
			return alternative, alternativeErr
		}
		return d, err
	}
	if r.constraints == nil {
		return d, nil
	}
	constraint, constraintErr := r.constraints.Evaluate(ctx, req)
	constraint.MatchedRules = appendUniqueRules(d.MatchedRules, constraint.MatchedRules...)
	return constraint, constraintErr
}

func (p *compiledPolicy) Vocabulary() Vocabulary    { return p.vocabulary.Copy() }
func (p *compiledPolicy) UsesAllowAuthorizer() bool { return p.allow }
func (p *compiledPolicy) FactSources() []IFactSource {
	var sources []IFactSource
	for _, rule := range p.rules {
		sources = append(sources, collectFactSources(rule.permission, rule.constraints, rule.alternative)...)
	}
	return sources
}

type scopePolicy struct {
	resolver   ISpecResolver
	vocabulary Vocabulary
}

func (p *scopePolicy) Evaluate(ctx context.Context, req Request) (Decision, error) {
	s, assign, err := p.resolver.ResolveSpec(ctx, req)
	if err != nil {
		return Decision{}, err
	}
	return Decision{Allowed: true, Spec: s, Assign: assign}, nil
}
func (p *scopePolicy) Vocabulary() Vocabulary     { return p.vocabulary.Copy() }
func (p *scopePolicy) FactSources() []IFactSource { return collectFactSources(p.resolver) }

// Copy 返回动作描述及维度清单的独立副本。
func (d Descriptor) Copy() Descriptor {
	d.Actions = append([]Action(nil), d.Actions...)
	for i := range d.Actions {
		d.Actions[i].Dimensions = append([]meta.Dimension(nil), d.Actions[i].Dimensions...)
	}
	return d
}

// Plan 返回声明编译的只读计划；低层策略装配返回未绑定状态。
func (a *Authorizer) Plan() (Plan, bool) {
	if a.plan == nil {
		return Plan{}, false
	}
	return a.plan.Copy(), true
}

// PlanOf 取得声明编译的独立计划描述。
func PlanOf(authorizer IAuthorizer) (Plan, bool) {
	if depcheck.IsNil(authorizer) {
		return Plan{}, false
	}
	provider, ok := authorizer.(interface{ Plan() (Plan, bool) })
	if !ok {
		return Plan{}, false
	}
	plan, bound := provider.Plan()
	return plan.Copy(), bound
}

// ValidatePlanBinding 核对已编译计划与执行入口；低层策略继续由消费端验证资源契约。
func ValidatePlanBinding(authorizer IAuthorizer, descriptor Descriptor, access ResourceAccess) error {
	plan, bound := PlanOf(authorizer)
	if !bound {
		return nil
	}
	if err := plan.Validate(); err != nil {
		return err
	}
	if plan.ResourceAccess != access {
		return errors.NewCode(errors.InvalidInput, "authorization plan resource access conflicts with execution binding")
	}
	if plan.Descriptor.Resource != descriptor.Resource || len(plan.Descriptor.Actions) != len(descriptor.Actions) {
		return errors.NewCode(errors.InvalidInput, "authorization plan descriptor conflicts with execution binding")
	}
	for _, actual := range descriptor.Actions {
		declared, ok := plan.Descriptor.Action(actual.Name)
		permission, _ := descriptor.PermissionFor(actual.Name)
		plannedPermission, _ := plan.Descriptor.PermissionFor(actual.Name)
		if !ok || permission != plannedPermission || actual.Kind != declared.Kind || actual.ReceiveAction != declared.ReceiveAction || !reflect.DeepEqual(actual.Dimensions, declared.Dimensions) {
			return errors.NewCode(errors.InvalidInput, "authorization action conflicts with execution binding").WithContext("action", actual.Name)
		}
	}
	return nil
}
