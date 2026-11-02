package auth_test

import (
	"context"
	"gochen/auth"
	"gochen/domain/meta"
	"gochen/domain/spec"
	"testing"
)

type stubAuthorizer struct {
	decision   auth.Decision
	err        error
	vocabulary auth.Vocabulary
}

func (s stubAuthorizer) Authorize(context.Context, auth.Request) (auth.Decision, error) {
	return s.decision, s.err
}

func (s stubAuthorizer) Vocabulary() auth.Vocabulary { return s.vocabulary }

func TestAllow(t *testing.T) {
	decision, err := auth.Allow().Authorize(context.Background(), auth.Request{})
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if !decision.Allowed || !decision.Spec.IsAll() {
		t.Fatalf("Allow decision = %+v", decision)
	}
}

func TestAllCombine(t *testing.T) {
	a := stubAuthorizer{
		decision:   auth.Decision{Allowed: true, Spec: spec.Eq(meta.ByField("A"), 1), Assign: []auth.Assignment{{Dimension: "owner", Value: "u1"}}},
		vocabulary: auth.Vocabulary{Refs: []meta.Ref{meta.ByDimension("owner"), meta.ByField("A")}},
	}
	b := stubAuthorizer{
		decision:   auth.Decision{Allowed: true, Spec: spec.Eq(meta.ByField("B"), 2)},
		vocabulary: auth.Vocabulary{Refs: []meta.Ref{meta.ByDimension("owner"), meta.ByDimension("scope")}},
	}
	combined, err := auth.All(a, b)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	decision, err := combined.Authorize(context.Background(), auth.Request{})
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("decision = %+v", decision)
	}
	if got := decision.Spec.Canonical(); got != "and(eq(field:A,1),eq(field:B,2))" {
		t.Fatalf("spec = %s", got)
	}
	if len(decision.Assign) != 1 || decision.Assign[0].Dimension != "owner" {
		t.Fatalf("assign = %+v", decision.Assign)
	}
	vocab := combined.Vocabulary()
	if len(vocab.Refs) != 3 {
		t.Fatalf("vocabulary = %+v", vocab)
	}
}

func TestAllDenyAndConflict(t *testing.T) {
	deny := stubAuthorizer{decision: auth.Decision{Allowed: false, Reason: "no_scope"}}
	combined, err := auth.All(stubAuthorizer{decision: auth.Decision{Allowed: true, Spec: spec.All()}}, deny)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	decision, err := combined.Authorize(context.Background(), auth.Request{})
	if err != nil || decision.Allowed || decision.Reason != "no_scope" {
		t.Fatalf("deny decision = %+v, %v", decision, err)
	}

	left := stubAuthorizer{decision: auth.Decision{Allowed: true, Spec: spec.All(), Assign: []auth.Assignment{{Dimension: "owner", Value: "u1"}}}}
	right := stubAuthorizer{decision: auth.Decision{Allowed: true, Spec: spec.All(), Assign: []auth.Assignment{{Dimension: "owner", Value: "u2"}}}}
	combined, err = auth.All(left, right)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	decision, err = combined.Authorize(context.Background(), auth.Request{})
	if err != nil || decision.Allowed || decision.Reason != "assignment_conflict" {
		t.Fatalf("conflict decision = %+v, %v", decision, err)
	}
}

func TestAllRequiresAuthorizer(t *testing.T) {
	if _, err := auth.All(); err == nil {
		t.Fatal("expected error for empty authorizers")
	}
	if _, err := auth.All(nil); err == nil {
		t.Fatal("expected error for nil authorizer")
	}
	var authorizer *stubAuthorizer
	if _, err := auth.All(authorizer); err == nil {
		t.Fatal("expected error for typed nil authorizer")
	}
}

func TestAllAllowIdentification(t *testing.T) {
	combined, err := auth.All(auth.Allow(), auth.Allow())
	if err != nil {
		t.Fatal(err)
	}
	if !auth.IsAllow(combined) {
		t.Fatal("All of built-in Allow policies must retain the startup marker")
	}
	mixed, err := auth.All(auth.Allow(), stubAuthorizer{decision: auth.Decision{Allowed: false}})
	if err != nil {
		t.Fatal(err)
	}
	if auth.IsAllow(mixed) {
		t.Fatal("All with a restricting policy must not be classified as Allow")
	}
}

func TestDescriptor(t *testing.T) {
	d := auth.Descriptor{
		Resource: "erp.order",
		Actions: []auth.Action{
			{Name: "read", Kind: auth.KindRead},
			{Name: "approve", Kind: auth.KindUpdate, Permission: "erp.order:api:approve"},
		},
	}
	if err := d.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if code, ok := d.PermissionFor("read"); !ok || code != "erp.order:api:read" {
		t.Fatalf("PermissionFor(read) = %q, %v", code, ok)
	}
	if code, ok := d.PermissionFor("approve"); !ok || code != "erp.order:api:approve" {
		t.Fatalf("PermissionFor(approve) = %q, %v", code, ok)
	}
	if _, ok := d.PermissionFor("missing"); ok {
		t.Fatal("PermissionFor(missing) should be false")
	}

	bad := auth.Descriptor{Resource: "r", Actions: []auth.Action{{Name: "read"}, {Name: "read"}}}
	if err := bad.Validate(); err == nil {
		t.Fatal("expected duplicate action error")
	}
}
