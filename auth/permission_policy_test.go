package auth_test

import (
	"context"
	"testing"

	"gochen/auth"
	"gochen/errors"
)

type fakeChecker struct {
	err   error
	calls []string
}

func (f *fakeChecker) RequireAction(_ context.Context, code string) error {
	f.calls = append(f.calls, code)
	return f.err
}

func TestRBAC(t *testing.T) {
	checker := &fakeChecker{}
	authorizer, err := auth.NewPermissionPolicy(checker)
	if err != nil {
		t.Fatalf("RBAC: %v", err)
	}
	decision, err := authorizer.Evaluate(context.Background(), auth.Request{Permission: "erp.order:api:read"})
	if err != nil || !decision.Allowed || !decision.Spec.IsAll() {
		t.Fatalf("allow decision = %+v, %v", decision, err)
	}
	if len(checker.calls) != 1 || checker.calls[0] != "erp.order:api:read" {
		t.Fatalf("checker calls = %v", checker.calls)
	}

	checker.err = errors.NewCode(errors.Forbidden, "denied")
	decision, err = authorizer.Evaluate(context.Background(), auth.Request{Permission: "erp.order:api:read"})
	if err != nil || decision.Allowed || decision.Reason != "forbidden" {
		t.Fatalf("forbidden decision = %+v, %v", decision, err)
	}

	checker.err = errors.NewCode(errors.Unauthorized, "no principal")
	if _, err := authorizer.Evaluate(context.Background(), auth.Request{Permission: "erp.order:api:read"}); err == nil {
		t.Fatal("expected unauthorized error")
	}

	checker.err = nil
	if _, err := authorizer.Evaluate(context.Background(), auth.Request{}); err == nil {
		t.Fatal("expected missing permission error")
	}
}

func TestRBACSystemBypass(t *testing.T) {
	checker := &fakeChecker{err: errors.NewCode(errors.Forbidden, "denied")}
	authorizer, err := auth.NewPermissionPolicy(checker)
	if err != nil {
		t.Fatalf("RBAC: %v", err)
	}
	ctx := auth.AsSystem(context.Background(), "test")
	decision, err := authorizer.Evaluate(ctx, auth.Request{Permission: "erp.order:api:read"})
	if err != nil || !decision.Allowed {
		t.Fatalf("system decision = %+v, %v", decision, err)
	}
	if len(checker.calls) != 0 {
		t.Fatalf("checker should not be called for system identity, calls = %v", checker.calls)
	}
}

func TestRBACRequiresChecker(t *testing.T) {
	if _, err := auth.NewPermissionPolicy(nil); err == nil {
		t.Fatal("expected nil checker error")
	}
	var checker *fakeChecker
	if _, err := auth.NewPermissionPolicy(checker); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("RBAC(typed nil) = %v, want InvalidInput", err)
	}
	var function auth.ActionCheckerFunc
	if _, err := auth.NewPermissionPolicy(function); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("RBAC(nil function) = %v, want InvalidInput", err)
	}
}
