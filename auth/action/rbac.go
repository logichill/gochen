package action

import (
	"context"

	"gochen/auth"
	"gochen/domain/spec"
	"gochen/errors"
)

type rbacAuthorizer struct {
	checker IActionChecker
}

// RBAC 返回只按权限码判定的授权器：规约为 spec.All()，无赋值与词汇。
func RBAC(checker IActionChecker) (auth.IAuthorizer, error) {
	if checker == nil {
		return nil, errors.NewCode(errors.InvalidInput, "action checker cannot be nil")
	}
	return &rbacAuthorizer{checker: checker}, nil
}

func (r *rbacAuthorizer) Authorize(ctx context.Context, req auth.Request) (auth.Decision, error) {
	if principal, ok := auth.PrincipalFromContext(ctx); ok && principal.System {
		return auth.Decision{Allowed: true, Spec: spec.All()}, nil
	}
	if req.Permission == "" {
		return auth.Decision{}, errors.NewCode(errors.InvalidInput, "request permission is required")
	}
	if err := RequireAction(ctx, r.checker, req.Permission); err != nil {
		if errors.Code(err) == errors.Forbidden {
			return auth.Decision{Allowed: false, Reason: "forbidden"}, nil
		}
		return auth.Decision{}, err
	}
	return auth.Decision{Allowed: true, Spec: spec.All()}, nil
}

func (r *rbacAuthorizer) Vocabulary() auth.Vocabulary { return auth.Vocabulary{} }
