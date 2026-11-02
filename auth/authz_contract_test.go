package auth_test

import (
	"context"
	"gochen/auth"
	"gochen/auth/action"
	"gochen/auth/scoped"
	"gochen/domain/meta"
	"gochen/domain/spec"
	"gochen/errors"
	"gochen/testkit/require"
	"testing"
)

type contractChecker struct {
	denied bool
	err    error
}

func (c contractChecker) RequireAction(context.Context, string) error {
	if c.err != nil {
		return c.err
	}
	if c.denied {
		return errors.NewCode(errors.Forbidden, "denied")
	}
	return nil
}

type contractResolver struct {
	spec        spec.Spec
	assignments []auth.Assignment
	err         error
}

func (r contractResolver) ResolveSpec(context.Context, auth.Request) (spec.Spec, []auth.Assignment, error) {
	if r.err != nil {
		return spec.Spec{}, nil, r.err
	}
	s := r.spec
	if s.IsNone() {
		s = spec.All()
	}
	return s, r.assignments, nil
}

func (r contractResolver) Vocabulary() auth.Vocabulary {
	return auth.Vocabulary{Refs: []meta.Ref{meta.ByDimension("owner"), meta.ByField("Owner")}}
}

func contractAuthorizers(t *testing.T) map[string]auth.IAuthorizer {
	t.Helper()
	rbac, err := action.RBAC(contractChecker{})
	require.NoError(t, err)
	full, err := scoped.New(contractChecker{}, contractResolver{spec: spec.Eq(meta.ByDimension("owner"), "u1")})
	require.NoError(t, err)
	deniedRBAC, err := action.RBAC(contractChecker{denied: true})
	require.NoError(t, err)
	return map[string]auth.IAuthorizer{
		"allow":  auth.Allow(),
		"rbac":   rbac,
		"scoped": full,
		"denied": deniedRBAC,
	}
}

func TestAuthorizerContractDecisionShape(t *testing.T) {
	ctx := context.Background()
	req := auth.Request{Resource: "task", Action: "read", Permission: "task:api:read"}
	auths := contractAuthorizers(t)

	allow := auths["allow"]
	decision, err := allow.Authorize(ctx, req)
	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.True(t, decision.Spec.IsAll())
	require.True(t, len(allow.Vocabulary().Refs) == 0)

	rbac := auths["rbac"]
	decision, err = rbac.Authorize(ctx, req)
	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.True(t, decision.Spec.IsAll())

	decision, err = auths["denied"].Authorize(ctx, req)
	require.NoError(t, err)
	require.False(t, decision.Allowed)
	require.True(t, decision.Reason != "")
}

func TestAuthorizerContractScopedProducesSpec(t *testing.T) {
	full := contractAuthorizers(t)["scoped"]
	decision, err := full.Authorize(context.Background(), auth.Request{
		Resource:   "task",
		Action:     "read",
		Permission: "task:api:read",
	})
	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.Equal(t, spec.Eq(meta.ByDimension("owner"), "u1").Canonical(), decision.Spec.Canonical())
	require.Equal(t, []meta.Ref{meta.ByDimension("owner"), meta.ByField("Owner")}, full.Vocabulary().Refs)
}

func TestAuthorizerContractSystemIdentityBypassesPolicy(t *testing.T) {
	ctx, err := auth.WithPrincipal(context.Background(), auth.Principal{SubjectID: "u1", System: true})
	require.NoError(t, err)

	for name, authorizer := range contractAuthorizers(t) {
		decision, err := authorizer.Authorize(ctx, auth.Request{
			Resource:   "task",
			Action:     "delete",
			Permission: "task:api:delete",
		})
		require.NoError(t, err)
		require.True(t, decision.Allowed, name)
		require.True(t, decision.Spec.IsAll(), name)
	}
}

func TestAuthorizerContractErrorsStayErrors(t *testing.T) {
	cause := errors.NewCode(errors.Internal, "checker unavailable")
	rbac, err := action.RBAC(contractChecker{err: cause})
	require.NoError(t, err)
	_, err = rbac.Authorize(context.Background(), auth.Request{Resource: "task", Action: "read", Permission: "task:api:read"})
	require.ErrorIs(t, err, cause)

	full, err := scoped.New(contractChecker{}, contractResolver{err: cause})
	require.NoError(t, err)
	_, err = full.Authorize(context.Background(), auth.Request{Resource: "task", Action: "read", Permission: "task:api:read"})
	require.ErrorIs(t, err, cause)
}
