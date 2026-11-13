package auth_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"gochen/auth"
	"gochen/contextx"
	"gochen/domain/meta"
	"gochen/domain/spec"
	"gochen/errors"
)

type ownerProjection struct {
	ID        string
	Owner     string
	Version   uint64
	Relations map[string][]string
}

func TestDerivedFactsReuseParentsAndRemainLazy(t *testing.T) {
	d, options := declarationFixture(t)
	parentLoads, childLoads := 0, 0
	parent, err := auth.NewFact("order", func(context.Context, auth.Request) (ownerProjection, error) {
		parentLoads++
		return ownerProjection{ID: "42", Owner: "alice", Relations: map[string][]string{"family": {"bob"}}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	child, err := auth.NewDerivedFact("order.product", parent, func(_ context.Context, _ auth.Request, order ownerProjection) (string, error) {
		childLoads++
		order.Relations["family"][0] = "mutation"
		return order.ID, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	selected := false
	policy, err := auth.WithFacts(auth.PolicyFunc(func(ctx context.Context, request auth.Request) (auth.Decision, error) {
		if selected {
			if _, err := child.Get(ctx, request); err != nil {
				return auth.Decision{}, err
			}
			order, err := parent.Get(ctx, request)
			if err != nil || order.Relations["family"][0] != "bob" {
				t.Fatalf("parent projection=%+v err=%v", order, err)
			}
		}
		return auth.Decision{Allowed: true, Spec: spec.All()}, nil
	}), child)
	if err != nil {
		t.Fatal(err)
	}
	d.Rules = []auth.Rule{{Constraints: []auth.IPolicy{policy}}}
	authorizer, err := auth.Compile(d, options)
	if err != nil {
		t.Fatal(err)
	}
	request := auth.Request{Resource: "document", Action: "read", IDs: []string{"42"}}
	if _, err := authorizer.Authorize(t.Context(), request); err != nil || parentLoads != 0 || childLoads != 0 {
		t.Fatalf("unselected facts=%d/%d err=%v", parentLoads, childLoads, err)
	}
	selected = true
	if _, err := authorizer.Authorize(t.Context(), request); err != nil || parentLoads != 1 || childLoads != 1 {
		t.Fatalf("derived facts=%d/%d err=%v", parentLoads, childLoads, err)
	}
	if _, err := auth.NewDerivedFact("invalid", (*auth.Fact[string])(nil), func(context.Context, auth.Request, string) (string, error) { return "", nil }); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("invalid parent=%v", err)
	}
}

func TestMultiResourceFactReuseBindsIdentityIntentAndProvider(t *testing.T) {
	tenantContext, err := contextx.WithTenantID(t.Context(), "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := auth.WithPrincipal(tenantContext, auth.Principal{SubjectID: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	loads := 0
	fact, err := auth.NewFact("order", func(context.Context, auth.Request) (ownerProjection, error) {
		loads++
		return ownerProjection{ID: "42", Owner: "alice", Relations: map[string][]string{"family": {"bob"}}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	compile := func(resource string, source *auth.Fact[ownerProjection]) *auth.Authorizer {
		t.Helper()
		d, options := declarationFixture(t)
		d.Descriptor.Resource = resource
		policy, err := auth.WithFacts(auth.PolicyFunc(func(ctx context.Context, request auth.Request) (auth.Decision, error) {
			projection, err := source.Get(ctx, request)
			if err == nil && projection.Relations["family"][0] != "bob" {
				t.Fatal("reused projection was mutated")
			}
			return auth.Decision{Allowed: err == nil, Spec: spec.All()}, err
		}), source)
		if err != nil {
			t.Fatal(err)
		}
		d.Rules = []auth.Rule{{Constraints: []auth.IPolicy{policy}}}
		authorizer, err := auth.Compile(d, options)
		if err != nil {
			t.Fatal(err)
		}
		return authorizer
	}
	order, points := compile("order", fact), compile("points", fact)
	request := auth.Request{Resource: "order", Action: "read", IDs: []string{"42"}}
	_, previous, err := order.AuthorizeRequest(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := fact.Get(ctx, previous)
	if err != nil {
		t.Fatal(err)
	}
	projection.Relations["family"][0] = "mutation"
	request.Resource = "points"
	prepared, err := points.ReuseFacts(ctx, request, previous)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := points.Authorize(ctx, prepared); err != nil || loads != 1 {
		t.Fatalf("shared provider loads=%d err=%v", loads, err)
	}
	changed := request
	changed.IDs = []string{"43"}
	if _, err := points.ReuseFacts(ctx, changed, previous); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("changed target=%v", err)
	}
	changed = request
	changed.Targets = []auth.Assignment{{Dimension: "scope", Value: int64(2)}}
	if _, err := points.ReuseFacts(ctx, changed, previous); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("changed intent=%v", err)
	}
	bob, err := auth.WithPrincipal(ctx, auth.Principal{SubjectID: "bob"})
	if err != nil {
		t.Fatal(err)
	}
	otherTenant, err := contextx.WithTenantID(ctx, "tenant-b")
	if err != nil {
		t.Fatal(err)
	}
	for _, changedCtx := range []context.Context{bob, otherTenant, auth.AsSystem(ctx, "test")} {
		if _, err := points.ReuseFacts(changedCtx, request, previous); !errors.Is(err, errors.InvalidInput) {
			t.Fatalf("changed identity=%v", err)
		}
	}
	if _, err := points.ReuseFacts(ctx, prepared, previous); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("already managed target=%v", err)
	}
	otherFact, err := auth.NewFact("order", func(context.Context, auth.Request) (ownerProjection, error) {
		loads++
		return ownerProjection{ID: "42", Relations: map[string][]string{"family": {"bob"}}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	other := compile("points", otherFact)
	prepared, err = other.ReuseFacts(ctx, request, previous)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Authorize(ctx, prepared); err != nil || loads != 2 {
		t.Fatalf("distinct provider loads=%d err=%v", loads, err)
	}
}

func TestFactsAreLazyAndIsolateTrustedProjection(t *testing.T) {
	d, options := declarationFixture(t)
	var loads atomic.Int32
	projection := ownerProjection{ID: "42", Owner: "alice", Version: 3, Relations: map[string][]string{"family": {"bob"}}}
	fact, err := auth.NewFact("document.owner", func(_ context.Context, req auth.Request) (ownerProjection, error) {
		loads.Add(1)
		if len(req.IDs) != 1 || req.IDs[0] != "42" {
			t.Fatalf("loader intent=%+v", req)
		}
		req.IDs[0] = "loader-mutation"
		return projection, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	own, err := auth.WithFacts(auth.PolicyFunc(func(ctx context.Context, req auth.Request) (auth.Decision, error) {
		p, err := fact.Get(ctx, req)
		if err != nil {
			return auth.Decision{}, err
		}
		p.Relations["family"][0] = "mutation"
		again, err := fact.Get(ctx, req)
		if err != nil {
			return auth.Decision{}, err
		}
		if again.Relations["family"][0] != "bob" {
			t.Fatal("projection shares mutable values")
		}
		return auth.Decision{Allowed: again.Owner == "alice", Spec: spec.All()}, nil
	}), fact)
	if err != nil {
		t.Fatal(err)
	}
	permissionErr := error(nil)
	options.Checker = auth.ActionCheckerFunc(func(context.Context, string) error { return permissionErr })
	d.Rules = []auth.Rule{{OnPermissionDenied: own}}
	a, err := auth.Compile(d, options)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := auth.WithPrincipal(context.Background(), auth.Principal{SubjectID: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	req := auth.Request{Resource: "document", Action: "read", IDs: []string{"42"}}
	if decision, err := a.Authorize(ctx, req); err != nil || !decision.Allowed || loads.Load() != 0 {
		t.Fatalf("unselected branch decision=%+v loads=%d err=%v", decision, loads.Load(), err)
	}
	permissionErr = errors.NewCode(errors.Internal, "permission store unavailable")
	if _, err := a.Authorize(ctx, req); !errors.Is(err, errors.Internal) || loads.Load() != 0 {
		t.Fatalf("fault=%v loads=%d", err, loads.Load())
	}
	permissionErr = errors.NewCode(errors.Forbidden, "permission denied")
	if decision, err := a.Authorize(ctx, req); err != nil || !decision.Allowed || loads.Load() != 1 {
		t.Fatalf("selected branch=%+v loads=%d err=%v", decision, loads.Load(), err)
	}
	if projection.Relations["family"][0] != "bob" || req.IDs[0] != "42" {
		t.Fatal("trusted loader or caller state mutated")
	}
	if _, err := fact.Get(ctx, req); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("unmanaged fact=%v", err)
	}
}

func TestPreparedFactsBindSourceIdentityAndIntent(t *testing.T) {
	d, options := declarationFixture(t)
	var loads atomic.Int32
	fact, err := auth.NewFact("document.owner", func(context.Context, auth.Request) (ownerProjection, error) {
		loads.Add(1)
		return ownerProjection{ID: "42", Owner: "alice", Version: 3}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	p, err := auth.WithFacts(auth.PolicyFunc(func(ctx context.Context, req auth.Request) (auth.Decision, error) {
		_, err := fact.Get(ctx, req)
		return auth.Decision{Allowed: err == nil, Spec: spec.All()}, err
	}), fact)
	if err != nil {
		t.Fatal(err)
	}
	d.Rules = []auth.Rule{{Constraints: []auth.IPolicy{p}}}
	a, err := auth.Compile(d, options)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := auth.WithPrincipal(context.Background(), auth.Principal{SubjectID: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := a.PrepareFacts(ctx, auth.Request{Resource: "document", Action: "read", IDs: []string{"42"}}, fact)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := fact.Get(ctx, prepared); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if decision, err := a.Authorize(ctx, prepared); err != nil || !decision.Allowed || loads.Load() != 1 {
		t.Fatalf("reuse=%+v loads=%d err=%v", decision, loads.Load(), err)
	}
	changed, err := prepared.Copy()
	if err != nil {
		t.Fatal(err)
	}
	changed.IDs[0] = "43"
	if _, err := a.Authorize(ctx, changed); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("changed ID=%v", err)
	}
	other, err := auth.Compile(d, options)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Authorize(ctx, prepared); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("different kernel=%v", err)
	}
	bob, err := auth.WithPrincipal(ctx, auth.Principal{SubjectID: "bob"})
	if err != nil {
		t.Fatal(err)
	}
	aliceAgain, err := auth.WithPrincipal(bob, auth.Principal{SubjectID: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	for _, changedCtx := range []context.Context{bob, aliceAgain, auth.AsSystem(ctx, "test")} {
		if _, err := a.Authorize(changedCtx, prepared); !errors.Is(err, errors.InvalidInput) {
			t.Fatalf("changed identity=%v", err)
		}
	}
}

func TestFactDependenciesFailDuringConstruction(t *testing.T) {
	for _, test := range []struct {
		name    string
		sources []auth.IFactSource
	}{
		{name: "nil", sources: []auth.IFactSource{nil}},
		{name: "typed nil", sources: []auth.IFactSource{(*auth.Fact[string])(nil)}},
		{name: "zero value", sources: []auth.IFactSource{&auth.Fact[string]{}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			p, err := auth.WithFacts(auth.AllowPolicy(), test.sources...)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := auth.New(auth.Options{Policy: p}); !errors.Is(err, errors.InvalidInput) {
				t.Fatalf("dependency=%v", err)
			}
		})
	}
	first, err := auth.NewFact("same", func(context.Context, auth.Request) (string, error) { return "one", nil })
	if err != nil {
		t.Fatal(err)
	}
	second, err := auth.NewFact("same", func(context.Context, auth.Request) (string, error) { return "two", nil })
	if err != nil {
		t.Fatal(err)
	}
	p, err := auth.WithFacts(auth.AllowPolicy(), first, second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.New(auth.Options{Policy: p}); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("conflicting source=%v", err)
	}
}

func TestAllPoliciesIsolatesNestedRequestIntent(t *testing.T) {
	first := auth.PolicyFunc(func(_ context.Context, req auth.Request) (auth.Decision, error) {
		req.IDs[0] = "mutation"
		req.Targets[0].Value.(map[string][]string)["ids"][0] = "mutation"
		return auth.Decision{Allowed: true, Spec: spec.All()}, nil
	})
	second := auth.PolicyFunc(func(_ context.Context, req auth.Request) (auth.Decision, error) {
		if req.IDs[0] != "42" || req.Targets[0].Value.(map[string][]string)["ids"][0] != "43" {
			t.Fatal("policy changed another policy's intent")
		}
		return auth.Decision{Allowed: true, Spec: spec.All()}, nil
	})
	p, err := auth.AllPolicies(first, second)
	if err != nil {
		t.Fatal(err)
	}
	a, err := auth.New(auth.Options{Policy: p})
	if err != nil {
		t.Fatal(err)
	}
	req := auth.Request{IDs: []string{"42"}, Targets: []auth.Assignment{{Dimension: meta.Dimension("scope"), Value: map[string][]string{"ids": {"43"}}}}}
	if d, err := a.Authorize(context.Background(), req); err != nil || !d.Allowed {
		t.Fatalf("decision=%+v err=%v", d, err)
	}
	if req.IDs[0] != "42" || req.Targets[0].Value.(map[string][]string)["ids"][0] != "43" {
		t.Fatal("caller intent changed")
	}
}

func TestAuthorizeClosesRetainedFactsOnSuccessDenialAndError(t *testing.T) {
	for _, outcome := range []string{"allow", "deny", "error"} {
		t.Run(outcome, func(t *testing.T) {
			var retained auth.Request
			fact, err := auth.NewFact("retained", func(context.Context, auth.Request) (string, error) { return "trusted", nil })
			if err != nil {
				t.Fatal(err)
			}
			policy, err := auth.WithFacts(auth.PolicyFunc(func(ctx context.Context, req auth.Request) (auth.Decision, error) {
				retained = req
				if _, err := fact.Get(ctx, req); err != nil {
					return auth.Decision{}, err
				}
				if outcome == "error" {
					return auth.Decision{}, errors.NewCode(errors.Dependency, "unavailable")
				}
				return auth.Decision{Allowed: outcome == "allow", Spec: spec.All()}, nil
			}), fact)
			if err != nil {
				t.Fatal(err)
			}
			a, err := auth.New(auth.Options{Policy: policy})
			if err != nil {
				t.Fatal(err)
			}
			_, _ = a.Authorize(t.Context(), auth.Request{Resource: "document", Action: "read"})
			if _, err := fact.Get(t.Context(), retained); !errors.Is(err, errors.FailedPrecondition) {
				t.Fatalf("retained fact remained usable: %v", err)
			}
		})
	}
}
