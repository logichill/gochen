package scoped_test

import (
	"context"
	"gochen/auth"
	"gochen/auth/scoped"
	"gochen/domain/meta"
	"gochen/domain/spec"
	"gochen/errors"
	"testing"
)

type allowChecker struct{}

func (allowChecker) RequireAction(context.Context, string) error { return nil }

type denyChecker struct{}

func (denyChecker) RequireAction(context.Context, string) error {
	return errors.NewCode(errors.Forbidden, "denied")
}

func TestNewRequiresDependencies(t *testing.T) {
	resolver := auth.SpecResolverFunc(func(context.Context, auth.Request) (spec.Spec, []auth.Assignment, error) {
		return spec.All(), nil, nil
	})
	if _, err := auth.NewPermissionPolicy(nil, auth.WithScope(resolver)); err == nil {
		t.Fatal("expected nil checker error")
	}
	if _, err := auth.NewPermissionPolicy(allowChecker{}, auth.WithScope(nil)); err == nil {
		t.Fatal("expected nil resolver error")
	}
	var checker *allowChecker
	if _, err := auth.NewPermissionPolicy(checker, auth.WithScope(resolver)); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("New(typed nil checker) = %v", err)
	}
	var pointerResolver *auth.SpecResolverFunc
	if _, err := auth.NewPermissionPolicy(allowChecker{}, auth.WithScope(pointerResolver)); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("New(typed nil resolver) = %v", err)
	}
	var nilFunction auth.SpecResolverFunc
	if _, err := auth.NewPermissionPolicy(allowChecker{}, auth.WithScope(nilFunction)); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("New(nil resolver function) = %v", err)
	}
}

func TestFullAuthorizer(t *testing.T) {
	resolver := auth.SpecResolverFunc(func(_ context.Context, req auth.Request) (spec.Spec, []auth.Assignment, error) {
		return scoped.ScopeIn("scope", 1, 2), []auth.Assignment{{Dimension: "scope", Value: 1}}, nil
	})
	authorizer, err := auth.NewPermissionPolicy(allowChecker{}, auth.WithScope(resolver))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	decision, err := authorizer.Evaluate(context.Background(), auth.Request{Action: "read", Permission: "erp.order:api:read"})
	if err != nil || !decision.Allowed {
		t.Fatalf("decision = %+v, %v", decision, err)
	}
	if got := decision.Spec.Canonical(); got != "in(dim:scope,1,2)" {
		t.Fatalf("spec = %s", got)
	}
	if len(decision.Assign) != 1 || decision.Assign[0].Dimension != "scope" {
		t.Fatalf("assign = %+v", decision.Assign)
	}

	authorizer, err = auth.NewPermissionPolicy(denyChecker{}, auth.WithScope(resolver))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	decision, err = authorizer.Evaluate(context.Background(), auth.Request{Action: "read", Permission: "erp.order:api:read"})
	if err != nil || decision.Allowed || decision.Reason != "forbidden" {
		t.Fatalf("deny decision = %+v, %v", decision, err)
	}
}

func TestFullAuthorizerRejectsNamed(t *testing.T) {
	resolver := auth.SpecResolverFunc(func(context.Context, auth.Request) (spec.Spec, []auth.Assignment, error) {
		return spec.Named("visibleTo", "u1"), nil, nil
	})
	authorizer, err := auth.NewPermissionPolicy(allowChecker{}, auth.WithScope(resolver))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := authorizer.Evaluate(context.Background(), auth.Request{Action: "update", Permission: "erp.order:api:update"}); err == nil {
		t.Fatal("expected named-on-write error")
	}
	if _, err := authorizer.Evaluate(context.Background(), auth.Request{Action: "list", Permission: "erp.order:api:list"}); !errors.Is(err, errors.Internal) {
		t.Fatalf("named authorization = %v", err)
	}
}

func TestFullAuthorizerSystemBypass(t *testing.T) {
	resolver := auth.SpecResolverFunc(func(context.Context, auth.Request) (spec.Spec, []auth.Assignment, error) {
		return spec.Eq(meta.ByField("Never"), "x"), nil, nil
	})
	authorizer, err := auth.NewPermissionPolicy(denyChecker{}, auth.WithScope(resolver))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	decision, err := authorizer.Evaluate(auth.AsSystem(context.Background(), "test"), auth.Request{Action: "update", Permission: "erp.order:api:update"})
	if err != nil || !decision.Allowed || !decision.Spec.IsAll() {
		t.Fatalf("system decision = %+v, %v", decision, err)
	}
}

func TestSpecHelpers(t *testing.T) {
	if got := scoped.ScopeIn("scope", 1, 2).Canonical(); got != "in(dim:scope,1,2)" {
		t.Fatalf("ScopeIn = %s", got)
	}
	if got := scoped.OwnerIs("owner", "u1").Canonical(); got != `eq(dim:owner,"u1")` {
		t.Fatalf("OwnerIs = %s", got)
	}
	if !scoped.ScopeIn("scope").IsNone() {
		t.Fatal("ScopeIn with no scope must match nothing")
	}
}
