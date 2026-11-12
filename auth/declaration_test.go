package auth_test

import (
	"context"
	"testing"

	"gochen/auth"
	"gochen/domain/meta"
	"gochen/domain/spec"
	"gochen/errors"
)

type declarationDocument struct {
	ID         string `entity:"id"`
	Department int64  `dim:"department"`
}

type declarationScope struct {
	calls *int
	err   error
	ref   meta.Ref
}

func (s declarationScope) ResolveSpec(context.Context, auth.Request) (spec.Spec, []auth.Assignment, error) {
	if s.calls != nil {
		*s.calls++
	}
	return spec.In(s.ref, int64(10), int64(20)), nil, s.err
}
func (s declarationScope) Vocabulary() auth.Vocabulary {
	return auth.Vocabulary{Refs: []meta.Ref{s.ref}}
}

func declarationFixture(t *testing.T) (auth.Declaration, auth.CompileOptions) {
	t.Helper()
	metadata, err := meta.Describe[*declarationDocument]()
	if err != nil {
		t.Fatal(err)
	}
	return auth.Declaration{Descriptor: auth.Descriptor{Resource: "document", Actions: []auth.Action{{Name: "read", Kind: auth.KindRead, Permission: "document:api:read"}}}, ResourceAccess: auth.ResourceAccessData}, auth.CompileOptions{Metadata: metadata, Checker: auth.ActionCheckerFunc(func(context.Context, string) error { return nil })}
}

func TestDeclarationRejectsInvalidBindings(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*auth.Declaration, *auth.CompileOptions)
	}{
		{"missing access", func(d *auth.Declaration, _ *auth.CompileOptions) { d.ResourceAccess = auth.ResourceAccessUnspecified }},
		{"missing metadata", func(_ *auth.Declaration, o *auth.CompileOptions) { o.Metadata = nil }},
		{"missing checker", func(_ *auth.Declaration, o *auth.CompileOptions) { o.Checker = nil }},
		{"unknown action", func(d *auth.Declaration, _ *auth.CompileOptions) {
			d.Rules = []auth.Rule{{Actions: []string{"missing"}}}
		}},
		{"duplicate action", func(d *auth.Declaration, _ *auth.CompileOptions) {
			d.Rules = []auth.Rule{{Actions: []string{"read", "read"}}}
		}},
		{"duplicate default", func(d *auth.Declaration, _ *auth.CompileOptions) { d.Rules = []auth.Rule{{}, {}} }},
		{"nil scope", func(d *auth.Declaration, _ *auth.CompileOptions) { d.Rules = []auth.Rule{{Scope: &auth.ScopeRule{}}} }},
		{"invalid ref", func(d *auth.Declaration, _ *auth.CompileOptions) {
			d.Rules = []auth.Rule{{Scope: &auth.ScopeRule{Resolver: declarationScope{ref: meta.ByField("Missing")}}}}
		}},
		{"invalid permission", func(d *auth.Declaration, _ *auth.CompileOptions) { d.Descriptor.Actions[0].Permission = "invalid" }},
		{"action-only metadata", func(d *auth.Declaration, _ *auth.CompileOptions) { d.ResourceAccess = auth.ResourceAccessNone }},
		{"action-only scope", func(d *auth.Declaration, o *auth.CompileOptions) {
			d.ResourceAccess = auth.ResourceAccessNone
			o.Metadata = nil
			d.Rules = []auth.Rule{{Scope: &auth.ScopeRule{Resolver: declarationScope{ref: meta.ByDimension("department")}}}}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			d, o := declarationFixture(t)
			test.change(&d, &o)
			if _, err := auth.Compile(d, o); !errors.Is(err, errors.InvalidInput) {
				t.Fatalf("Compile error = %v", err)
			}
		})
	}
}

func TestDeclarationFreezesActionsAndUsesAuthoritativePermissions(t *testing.T) {
	d, o := declarationFixture(t)
	var checked string
	o.Checker = auth.ActionCheckerFunc(func(_ context.Context, code string) error { checked = code; return nil })
	a, err := auth.Compile(d, o)
	if err != nil {
		t.Fatal(err)
	}
	d.Descriptor.Actions[0].Permission = "other:api:read"
	plan, bound := a.Plan()
	if !bound {
		t.Fatal("missing plan")
	}
	plan.Descriptor.Actions[0].Permission = "other:api:read"
	plan.FastDenyPermissions["read"] = "other:api:read"
	decision, err := a.Authorize(context.Background(), auth.Request{Resource: "document", Action: "read"})
	if err != nil || !decision.Allowed || checked != "document:api:read" {
		t.Fatalf("decision=%+v code=%s err=%v", decision, checked, err)
	}
	if _, err := a.Authorize(context.Background(), auth.Request{Resource: "document", Action: "read", Permission: "other:api:read"}); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("permission override = %v", err)
	}
	if _, err := a.Authorize(context.Background(), auth.Request{Resource: "document", Action: "missing"}); !errors.Is(err, errors.Forbidden) {
		t.Fatalf("unknown action = %v", err)
	}
}

func TestDeclarationResolvesOnlySelectedBranch(t *testing.T) {
	d, o := declarationFixture(t)
	permissionErr := errors.NewCode(errors.Forbidden, "denied")
	o.Checker = auth.ActionCheckerFunc(func(context.Context, string) error { return permissionErr })
	scopeCalls, alternativeCalls := 0, 0
	scopeFailure := errors.NewCode(errors.Forbidden, "scope denied")
	d.Rules = []auth.Rule{{ID: "order.manage", Scope: &auth.ScopeRule{Resolver: declarationScope{calls: &scopeCalls, ref: meta.ByDimension("department"), err: scopeFailure}}, OnPermissionDenied: auth.PolicyFunc(func(context.Context, auth.Request) (auth.Decision, error) {
		alternativeCalls++
		return auth.Decision{Allowed: true, Spec: spec.All(), MatchedRules: []string{"order.own"}}, nil
	})}}
	a, err := auth.Compile(d, o)
	if err != nil {
		t.Fatal(err)
	}
	req := auth.Request{Resource: "document", Action: "read"}
	decision, err := a.Authorize(context.Background(), req)
	if err != nil || !decision.Allowed || scopeCalls != 0 || alternativeCalls != 1 {
		t.Fatalf("alternative=%+v err=%v scope=%d own=%d", decision, err, scopeCalls, alternativeCalls)
	}
	if len(decision.MatchedRules) != 2 || decision.MatchedRules[0] != "order.manage" || decision.MatchedRules[1] != "order.own" {
		t.Fatalf("rules=%v", decision.MatchedRules)
	}
	permissionErr = nil
	if _, err = a.Authorize(context.Background(), req); !errors.Is(err, scopeFailure) || scopeCalls != 1 || alternativeCalls != 1 {
		t.Fatalf("scope rejection=%v scope=%d own=%d", err, scopeCalls, alternativeCalls)
	}
	permissionErr = errors.NewCode(errors.Internal, "checker unavailable")
	if _, err = a.Authorize(context.Background(), req); !errors.Is(err, errors.Internal) || alternativeCalls != 1 {
		t.Fatalf("checker fault=%v own=%d", err, alternativeCalls)
	}
}

func TestAllPoliciesPropagatesExecutedRulesAndStops(t *testing.T) {
	for _, fault := range []bool{false, true} {
		thirdCalls := 0
		first := auth.PolicyFunc(func(context.Context, auth.Request) (auth.Decision, error) {
			return auth.Decision{Allowed: true, Spec: spec.All(), MatchedRules: []string{"one", "shared"}}, nil
		})
		second := auth.PolicyFunc(func(context.Context, auth.Request) (auth.Decision, error) {
			var err error
			if fault {
				err = errors.NewCode(errors.Internal, "fault")
			}
			return auth.Decision{Spec: spec.All(), MatchedRules: []string{"shared", "two"}}, err
		})
		third := auth.PolicyFunc(func(context.Context, auth.Request) (auth.Decision, error) {
			thirdCalls++
			return auth.Decision{Allowed: true, Spec: spec.All()}, nil
		})
		policy, err := auth.AllPolicies(first, second, third)
		if err != nil {
			t.Fatal(err)
		}
		a, err := auth.New(auth.Options{Policy: policy})
		if err != nil {
			t.Fatal(err)
		}
		d, err := a.Authorize(context.Background(), auth.Request{})
		if fault != (err != nil) || d.Allowed || !d.Spec.IsNone() || len(d.Assign) > 0 || thirdCalls != 0 {
			t.Fatalf("decision=%+v err=%v third=%d", d, err, thirdCalls)
		}
		if len(d.MatchedRules) != 3 || d.MatchedRules[0] != "one" || d.MatchedRules[1] != "shared" || d.MatchedRules[2] != "two" {
			t.Fatalf("rules=%v", d.MatchedRules)
		}
		if !fault && d.Reason != "forbidden" {
			t.Fatalf("reason=%s", d.Reason)
		}
	}
}

func TestKernelRejectsInvalidResultsAndOrphanGovernance(t *testing.T) {
	for _, g := range []auth.GovernanceOptions{{StrictAudit: true}, {LenientSnapshot: true}} {
		if _, err := auth.New(auth.Options{Policy: auth.AllowPolicy(), Governance: g}); !errors.Is(err, errors.InvalidInput) {
			t.Fatalf("orphan governance=%v", err)
		}
	}
	for _, d := range []auth.Decision{{Allowed: true, Spec: spec.Named("hidden")}, {Allowed: true, Spec: spec.All(), Assign: []auth.Assignment{{Dimension: "owner", Value: "alice"}}}} {
		a, err := auth.New(auth.Options{Policy: auth.PolicyFunc(func(context.Context, auth.Request) (auth.Decision, error) { return d, nil })})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := a.Authorize(context.Background(), auth.Request{}); !errors.Is(err, errors.Internal) {
			t.Fatalf("invalid result=%v", err)
		}
	}
}
