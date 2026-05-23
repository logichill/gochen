package auth

import (
	"context"
	"strings"
	"time"

	"gochen/errors"
)

// IAuthorizer 定义统一授权入口。
type IAuthorizer interface {
	Authorize(ctx context.Context, permission string, targets ...any) (AuthzDecision, error)
	Require(ctx context.Context, permission string, targets ...any) error
}

// IEvaluator 定义最小授权评估器接口。
type IEvaluator interface {
	Evaluate(ctx context.Context, principal Principal, permission string, resources []Resource) (AuthzDecision, error)
}

// EvaluatorFunc 允许使用函数直接实现 IEvaluator。
type EvaluatorFunc func(ctx context.Context, principal Principal, permission string, resources []Resource) (AuthzDecision, error)

// Evaluate 执行授权评估。
func (f EvaluatorFunc) Evaluate(ctx context.Context, principal Principal, permission string, resources []Resource) (AuthzDecision, error) {
	return f(ctx, principal, permission, resources)
}

// Authorizer 组合主体解析、资源解析与评估器。
type Authorizer struct {
	resourceResolver IResourceResolver
	evaluator        IEvaluator
}

// NewAuthorizer 创建最小授权器。
func NewAuthorizer(evaluator IEvaluator, resolver IResourceResolver) (*Authorizer, error) {
	if evaluator == nil {
		return nil, errors.NewCode(errors.InvalidInput, "evaluator cannot be nil")
	}
	return &Authorizer{resourceResolver: resolver, evaluator: evaluator}, nil
}

// Authorize 对目标资源执行授权评估。
func (a *Authorizer) Authorize(ctx context.Context, permission string, targets ...any) (AuthzDecision, error) {
	start := time.Now()
	permission = strings.TrimSpace(permission)
	if permission == "" {
		return AuthzDecision{}, errors.NewCode(errors.InvalidInput, "permission is required")
	}
	resources, err := ResolveResources(a.resourceResolver, targets...)
	if err != nil {
		return AuthzDecision{}, err
	}
	ctx, eval, err := BindAuthzEvalContextOrEmpty(ctx)
	if err != nil {
		return AuthzDecision{}, err
	}
	var decision AuthzDecision
	defer func() {
		recordAuthorizationOutcome(ctx, eval, permission, resources, decision, time.Since(start), err)
	}()
	resources, err = enrichResourcesWithDataScope(ctx, eval, resources)
	if err != nil {
		return AuthzDecision{}, err
	}
	if err := validateConsistencyForAuthorization(ctx, eval, permission, resources); err != nil {
		return AuthzDecision{}, err
	}
	if isZeroPrincipal(eval.Principal) {
		decision = denyWithEval(ReasonCodePrincipalMissing, eval, resources...)
		return decision, nil
	}
	decision, err = a.evaluator.Evaluate(ctx, eval.Principal, permission, resources)
	if err != nil {
		return AuthzDecision{}, err
	}
	decision = normalizeDecision(decision)
	decision = applyEvalToDecision(decision, eval)
	if decision.Effect == "" {
		decision.Effect = EffectDeny
	}
	if decision.Effect == EffectAllow && len(decision.AuthorizedResources) == 0 {
		decision.AuthorizedResources = cloneResources(resources)
	}
	decision.AuthorizedResources, err = enrichResourcesWithDataScope(ctx, eval, decision.AuthorizedResources)
	if err != nil {
		return AuthzDecision{}, err
	}
	return decision, nil
}

// Require 在 allow 之外直接返回统一错误。
func (a *Authorizer) Require(ctx context.Context, permission string, targets ...any) error {
	decision, err := a.Authorize(ctx, permission, targets...)
	if err != nil {
		return err
	}
	return decision.RequireAllow()
}

func enrichResourcesWithDataScope(ctx context.Context, eval AuthzEvalContext, resources []Resource) ([]Resource, error) {
	resources = cloneResources(resources)
	if len(resources) == 0 {
		return resources, nil
	}
	if !resourcesNeedDataScopeEnrichment(eval, resources) {
		return resources, nil
	}
	scope, err := resolveDataScopeForResources(ctx, eval)
	if err != nil {
		return nil, err
	}
	singleVisibleScopeID, hasSingleVisibleScope := dataScopeSingleVisibleScope(scope)
	// 收紧自动回填：
	//   - DataScope 为显式绑定（非 derived）且恰为单 scope 时，信任调用方显式意图；
	//   - DataScope 是从 Principal 推导的单 scope，且与 Principal.ActiveScopeID 一致时才回填。
	// 其他情况（multi-scope 可见、推导与 Principal 不一致）保持 ManagedScopeID=0，
	// 交由 evaluator 决策，避免"框架猜 scope"造成的越权面。
	canEnrich := false
	if hasSingleVisibleScope {
		if isDataScopeExplicitlyBound(ctx) {
			canEnrich = true
		} else if eval.Principal.ActiveScopeID > 0 && eval.Principal.ActiveScopeID == singleVisibleScopeID {
			canEnrich = true
		}
	}
	for i := range resources {
		if resources[i].GlobalScope {
			resources[i] = normalizeResource(resources[i])
			continue
		}
		if resources[i].ManagedScopeID == 0 && canEnrich {
			resources[i].ManagedScopeID = singleVisibleScopeID
		}
		resources[i] = normalizeResource(resources[i])
	}
	return resources, nil
}

func isDataScopeExplicitlyBound(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	if _, ok := DataScopeFromContext(ctx); !ok {
		return false
	}
	return !dataScopeIsDerivedFromContext(ctx)
}

func resolveDataScopeForResources(ctx context.Context, eval AuthzEvalContext) (DataScope, error) {
	if scope, ok := DataScopeFromContext(ctx); ok {
		return scope, nil
	}
	if !evalSuggestsDataScope(eval) {
		return DataScope{}, nil
	}
	return PrincipalDataScopeResolver{}.Resolve(ctx, eval)
}

func resourcesNeedDataScopeEnrichment(eval AuthzEvalContext, resources []Resource) bool {
	if !evalSuggestsDataScope(eval) {
		return false
	}
	for _, resource := range resources {
		r := normalizeResource(resource)
		if r.GlobalScope {
			continue
		}
		if r.ManagedScopeID == 0 {
			return true
		}
	}
	return false
}

func evalSuggestsDataScope(eval AuthzEvalContext) bool {
	principal := eval.Principal.Clone()
	return principal.ActiveScopeID > 0 || principal.IsSystem
}

func dataScopeSingleVisibleScope(scope DataScope) (int64, bool) {
	scope = normalizeDataScope(scope)
	if scope.Mode == ScopeModeGlobal {
		return 0, false
	}
	if len(scope.VisibleScopeIDs) == 1 {
		return scope.VisibleScopeIDs[0], true
	}
	return 0, false
}

func applyEvalToDecision(decision AuthzDecision, eval AuthzEvalContext) AuthzDecision {
	if decision.SnapshotVersion == "" {
		decision.SnapshotVersion = eval.SnapshotVersion
	}
	if eval.Consistency != ConsistencyModeUnspecified {
		decision.Consistency = eval.Consistency
	} else if decision.Consistency == ConsistencyModeUnspecified {
		decision.Consistency = ConsistencyModeStrong
	}
	return normalizeDecision(decision)
}

func denyWithEval(reasonCode string, eval AuthzEvalContext, resources ...Resource) AuthzDecision {
	decision := DenyDecision(reasonCode, resources...)
	return applyEvalToDecision(decision, eval)
}

func validateConsistencyForAuthorization(ctx context.Context, eval AuthzEvalContext, permission string, resources []Resource) error {
	if !IsHighRiskAuthorizationFromContext(ctx) {
		return nil
	}
	if normalizeConsistencyMode(eval.Consistency) == ConsistencyModeBoundedStaleness {
		return errors.NewCode(errors.ServiceUnavailable, "high-risk authorization requires strong consistency").
			WithContext("permission", strings.TrimSpace(permission)).
			WithContext("resource_count", len(resources)).
			WithContext("consistency", string(eval.Consistency))
	}
	return nil
}
