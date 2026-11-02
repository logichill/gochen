package scoped

import (
	"context"
	"gochen/auth"
	"gochen/auth/action"
	"gochen/domain/meta"
	"gochen/domain/spec"
	"gochen/errors"
	"gochen/internal/depcheck"
)

// ISpecResolver 是数据权限的扩展点：根据请求产出规约与服务端赋值。
type ISpecResolver interface {
	ResolveSpec(ctx context.Context, req auth.Request) (spec.Spec, []auth.Assignment, error)
	Vocabulary() auth.Vocabulary
}

// SpecResolverFunc 允许用函数实现 ISpecResolver。
type SpecResolverFunc func(ctx context.Context, req auth.Request) (spec.Spec, []auth.Assignment, error)

func (f SpecResolverFunc) ResolveSpec(ctx context.Context, req auth.Request) (spec.Spec, []auth.Assignment, error) {
	return f(ctx, req)
}

func (SpecResolverFunc) Vocabulary() auth.Vocabulary { return auth.Vocabulary{} }

// FullAuthorizer 组合权限码与数据范围：先校验权限，再解析规约与赋值。
type FullAuthorizer struct {
	checker    action.IActionChecker
	resolver   ISpecResolver
	vocabulary auth.Vocabulary
}

// New 创建完整授权器。
func New(checker action.IActionChecker, resolver ISpecResolver) (*FullAuthorizer, error) {
	if depcheck.IsNil(checker) {
		return nil, errors.NewCode(errors.InvalidInput, "action checker cannot be nil")
	}
	if depcheck.IsNil(resolver) {
		return nil, errors.NewCode(errors.InvalidInput, "spec resolver cannot be nil")
	}
	return &FullAuthorizer{
		checker:    checker,
		resolver:   resolver,
		vocabulary: resolver.Vocabulary().Copy(),
	}, nil
}

func (a *FullAuthorizer) Authorize(ctx context.Context, req auth.Request) (auth.Decision, error) {
	if principal, ok := auth.PrincipalFromContext(ctx); ok && principal.System {
		return auth.Decision{Allowed: true, Spec: spec.All()}, nil
	}
	if req.Permission == "" {
		return auth.Decision{}, errors.NewCode(errors.InvalidInput, "request permission is required")
	}
	if err := action.RequireAction(ctx, a.checker, req.Permission); err != nil {
		if errors.Code(err) == errors.Forbidden {
			return auth.Decision{Allowed: false, Reason: "forbidden"}, nil
		}
		return auth.Decision{}, err
	}

	spec, assignments, err := a.resolver.ResolveSpec(ctx, req)
	if err != nil {
		return auth.Decision{}, err
	}
	if spec.Err() != nil {
		return auth.Decision{}, errors.Wrap(spec.Err(), errors.Internal, "resolver returned invalid spec")
	}
	if spec.HasNamed() {
		return auth.Decision{}, errors.NewCode(errors.Internal, "authorization spec cannot contain named conditions").
			WithContext("action", req.Action)
	}
	for _, assignment := range assignments {
		if err := assignment.Validate(); err != nil {
			return auth.Decision{}, errors.Wrap(err, errors.Internal, "resolver returned an invalid assignment")
		}
	}
	return auth.Decision{Allowed: true, Spec: spec, Assign: assignments}, nil
}

func (a *FullAuthorizer) Vocabulary() auth.Vocabulary { return a.vocabulary.Copy() }

// ScopeIn 返回主体范围集合内的正谓词。
func ScopeIn(dimension meta.Dimension, scopeIDs ...int64) spec.Spec {
	return spec.In(meta.ByDimension(dimension), int64sToAny(scopeIDs)...)
}

// OwnerIs 返回归属主体为 owner 的正谓词。
func OwnerIs(dimension meta.Dimension, owner string) spec.Spec {
	return spec.Eq(meta.ByDimension(dimension), owner)
}

func int64sToAny(values []int64) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}
