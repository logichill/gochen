package auth_test

import (
	"context"
	"testing"

	"gochen/auth"
	"gochen/errors"
)

// transparentAuthorizer 演示自定义包装对已编译授权能力的完整转发。
type transparentAuthorizer struct {
	inner *auth.Authorizer
}

func (a transparentAuthorizer) Authorize(ctx context.Context, req auth.Request) (auth.Decision, error) {
	return a.inner.Authorize(ctx, req)
}

func (a transparentAuthorizer) Vocabulary() auth.Vocabulary { return a.inner.Vocabulary() }
func (a transparentAuthorizer) Plan() (auth.Plan, bool)     { return a.inner.Plan() }
func (a transparentAuthorizer) UsesAllowAuthorizer() bool   { return a.inner.UsesAllowAuthorizer() }
func (a transparentAuthorizer) FastDeny(ctx context.Context, action string) error {
	return a.inner.FastDeny(ctx, action)
}

func TestWrapperPreservesExecutionCapabilities(t *testing.T) {
	d, options := declarationFixture(t)
	checks := 0
	options.Checker = auth.ActionCheckerFunc(func(context.Context, string) error {
		checks++
		return errors.NewCode(errors.Forbidden, "denied")
	})
	inner, err := auth.Compile(d, options)
	if err != nil {
		t.Fatal(err)
	}
	wrapped := transparentAuthorizer{inner: inner}
	if _, err := auth.BindExecution(wrapped, d.Descriptor, d.ResourceAccess, "wrapped document", "Application.Get -> Repository.Get", "read"); err != nil {
		t.Fatal(err)
	}
	conflict := d.Descriptor
	conflict.Resource = "other"
	if err := auth.ValidatePlanBinding(wrapped, conflict, d.ResourceAccess); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("wrapped plan mismatch: %v", err)
	}
	plan, bound := auth.PlanOf(wrapped)
	if !bound {
		t.Fatal("wrapped plan is unbound")
	}
	plan.Descriptor.Resource = "mutated"
	if err := auth.ValidatePlanBinding(wrapped, d.Descriptor, d.ResourceAccess); err != nil {
		t.Fatalf("plan copy changed wrapped plan: %v", err)
	}
	if err := auth.CheckFastDeny(context.Background(), wrapped, "read"); !errors.Is(err, errors.Forbidden) || checks != 1 {
		t.Fatalf("wrapped FastDeny: checks=%d err=%v", checks, err)
	}
	decision, err := wrapped.Authorize(context.Background(), auth.Request{Resource: "document", Action: "read"})
	if decision.Allowed || err != nil || checks != 2 {
		t.Fatalf("wrapped full authorization: decision=%+v checks=%d err=%v", decision, checks, err)
	}
	d.Rules = []auth.Rule{{AllowReason: "explicit test fixture access"}}
	allow, err := auth.Compile(d, options)
	if err != nil {
		t.Fatal(err)
	}
	if !auth.IsAllow(transparentAuthorizer{inner: allow}) || auth.IsAllow(wrapped) {
		t.Fatal("wrapper lost explicit allow classification")
	}
}
