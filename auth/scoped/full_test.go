package scoped_test

import (
	"context"
	"testing"

	"gochen/auth"
	"gochen/auth/scoped"
	"gochen/domain/spec"
	"gochen/errors"
)

type allowChecker struct{}

func (allowChecker) RequireAction(context.Context, string) error { return nil }

type denyChecker struct{}

func (denyChecker) RequireAction(context.Context, string) error {
	return errors.NewCode(errors.Forbidden, "denied")
}

func TestNewRequiresDependencies(t *testing.T) {
	resolver := scoped.SpecResolverFunc(func(context.Context, auth.Request) (spec.Spec, []auth.Assignment, error) {
		return spec.All(), nil, nil
	})
	if _, err := scoped.New(nil, resolver); err == nil {
		t.Fatal("expected nil checker error")
	}
	if _, err := scoped.New(allowChecker{}, nil); err == nil {
		t.Fatal("expected nil resolver error")
	}
}

func TestFullAuthorizer(t *testing.T) {
	resolver := scoped.SpecResolverFunc(func(_ context.Context, req auth.Request) (spec.Spec, []auth.Assignment, error) {
		return scoped.ScopeIn(1, 2), []auth.Assignment{{Role: spec.RoleScope, Value: 1}}, nil
	})
	authorizer, err := scoped.New(allowChecker{}, resolver)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	decision, err := authorizer.Authorize(context.Background(), auth.Request{Action: "read", Permission: "erp.order:api:read"})
	if err != nil || !decision.Allowed {
		t.Fatalf("decision = %+v, %v", decision, err)
	}
	if got := decision.Spec.Canonical(); got != "in(@scope,1,2)" {
		t.Fatalf("spec = %s", got)
	}
	if len(decision.Assign) != 1 || decision.Assign[0].Role != spec.RoleScope {
		t.Fatalf("assign = %+v", decision.Assign)
	}

	authorizer, err = scoped.New(denyChecker{}, resolver)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	decision, err = authorizer.Authorize(context.Background(), auth.Request{Action: "read", Permission: "erp.order:api:read"})
	if err != nil || decision.Allowed || decision.Reason != "forbidden" {
		t.Fatalf("deny decision = %+v, %v", decision, err)
	}
}

func TestFullAuthorizerRejectsNamedOnWrite(t *testing.T) {
	resolver := scoped.SpecResolverFunc(func(context.Context, auth.Request) (spec.Spec, []auth.Assignment, error) {
		return spec.Named("visibleTo", "u1"), nil, nil
	})
	authorizer, err := scoped.New(allowChecker{}, resolver)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := authorizer.Authorize(context.Background(), auth.Request{Action: "update", Permission: "erp.order:api:update"}); err == nil {
		t.Fatal("expected named-on-write error")
	}
	decision, err := authorizer.Authorize(context.Background(), auth.Request{Action: "list", Permission: "erp.order:api:list"})
	if err != nil || !decision.Allowed {
		t.Fatalf("read with named = %+v, %v", decision, err)
	}
}

func TestFullAuthorizerSystemBypass(t *testing.T) {
	resolver := scoped.SpecResolverFunc(func(context.Context, auth.Request) (spec.Spec, []auth.Assignment, error) {
		return spec.Eq("Never", "x"), nil, nil
	})
	authorizer, err := scoped.New(denyChecker{}, resolver)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	decision, err := authorizer.Authorize(auth.AsSystem(context.Background(), "test"), auth.Request{Action: "update", Permission: "erp.order:api:update"})
	if err != nil || !decision.Allowed || !decision.Spec.IsAll() {
		t.Fatalf("system decision = %+v, %v", decision, err)
	}
}

func TestSpecHelpers(t *testing.T) {
	if got := scoped.ScopeIn(1, 2).Canonical(); got != "in(@scope,1,2)" {
		t.Fatalf("ScopeIn = %s", got)
	}
	if got := scoped.OwnerIs("u1").Canonical(); got != `eq(@owner,"u1")` {
		t.Fatalf("OwnerIs = %s", got)
	}
	if !scoped.ScopeIn().IsNone() {
		t.Fatal("ScopeIn() with no scope must match nothing")
	}
}
