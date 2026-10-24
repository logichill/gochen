package scoped

import (
	"context"
	"strings"

	"gochen/errors"
)

// IAuthorizer 是 PDP 决策内核的统一入口。
//
// L4 治理能力（快照/审计/指标）以 Decorator 形式包装本接口，
// 而不是包装 Application——治理不使用时完全不参与编译。
type IAuthorizer interface {
	// Authorize 对目标资源做出授权判定。
	Authorize(ctx context.Context, action string, targets ...any) (Decision, error)
	// Require 在判定为 deny 时返回错误。
	Require(ctx context.Context, action string, targets ...any) error
}

// IEvaluator 是最小授权评估器。
type IEvaluator interface {
	Evaluate(ctx context.Context, action string, resources []Resource) (Decision, error)
}

// EvaluatorFunc 允许用函数直接实现 IEvaluator。
type EvaluatorFunc func(ctx context.Context, action string, resources []Resource) (Decision, error)

// Evaluate 执行授权评估。
func (f EvaluatorFunc) Evaluate(ctx context.Context, action string, resources []Resource) (Decision, error) {
	if f == nil {
		return Deny(ReasonPrincipalMissing), errors.NewCode(
			errors.InvalidInput,
			"evaluator function cannot be nil; this indicates a wiring bug",
		)
	}
	return f(ctx, action, resources)
}

// Authorizer 把资源解析与评估组合成标准 IAuthorizer。
type Authorizer struct {
	resolver  IResourceResolver
	evaluator IEvaluator
}

var _ IAuthorizer = (*Authorizer)(nil)

// NewAuthorizer 创建标准授权器。
//
// evaluator 为 nil 时返回错误——绝不构造一个"永远放行"的授权器。
func NewAuthorizer(resolver IResourceResolver, evaluator IEvaluator) (*Authorizer, error) {
	if evaluator == nil {
		return nil, errors.NewCode(errors.InvalidInput, "evaluator cannot be nil")
	}
	return &Authorizer{resolver: resolver, evaluator: evaluator}, nil
}

// Authorize 解析目标资源后交由评估器判定。
func (a *Authorizer) Authorize(ctx context.Context, action string, targets ...any) (Decision, error) {
	if a == nil || a.evaluator == nil {
		return Deny(ReasonPrincipalMissing), errors.NewCode(errors.Forbidden, "authorizer is not configured")
	}
	action = strings.TrimSpace(action)
	if action == "" {
		return Deny(ReasonActionDenied), errors.NewCode(errors.InvalidInput, "action cannot be empty")
	}
	resources, err := ResolveResources(a.resolver, targets...)
	if err != nil {
		return Deny(ReasonNoResource), err
	}
	decision, err := a.evaluator.Evaluate(ctx, action, resources)
	if err != nil {
		return Deny(decision.ReasonCode), err
	}
	return decision, nil
}

// Require 在判定为 deny 时返回 Forbidden。
//
// 无具体目标（动作级鉴权）时按 IsActionAllowed 判定，
// 避免“空 allow 无授权资源”判据误杀动作级授权。
func (a *Authorizer) Require(ctx context.Context, action string, targets ...any) error {
	decision, err := a.Authorize(ctx, action, targets...)
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
