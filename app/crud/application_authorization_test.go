package crud_test

import (
	"context"
	"testing"

	"gochen/app/crud"
	"gochen/auth"
	domcrud "gochen/domain/crud"
	"gochen/domain/meta"
	"gochen/domain/spec"
	"gochen/errors"
	"gochen/testkit"
)

type declaredOrder struct {
	domcrud.Entity[int64]
	Department int64 `dim:"department"`
	Name       string
}

type orderDepartmentScope struct{ calls *int }

func (s orderDepartmentScope) ResolveSpec(context.Context, auth.Request) (spec.Spec, []auth.Assignment, error) {
	*s.calls++
	return spec.Eq(meta.ByDimension("department"), int64(10)), nil, nil
}
func (orderDepartmentScope) Vocabulary() auth.Vocabulary {
	return auth.Vocabulary{Refs: []meta.Ref{meta.ByDimension("department")}}
}

func TestDeclarationAutomaticallyProtectsQueryCommandsAndRollback(t *testing.T) {
	ctx, err := auth.WithPrincipal(context.Background(), auth.Principal{SubjectID: "member"})
	if err != nil {
		t.Fatal(err)
	}
	repo, err := testkit.NewMemoryRepository[*declaredOrder, int64](nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, order := range []*declaredOrder{{Entity: domcrud.Entity[int64]{ID: 1}, Department: 10, Name: "inside"}, {Entity: domcrud.Entity[int64]{ID: 2}, Department: 20, Name: "outside"}} {
		if err := repo.Create(ctx, order, spec.All()); err != nil {
			t.Fatal(err)
		}
	}
	options := crud.Options[*declaredOrder, int64]{Resource: "order", ActionPermissions: map[string]string{"read": "order:api:read", "list": "order:api:read", "create": "order:api:write", "update": "order:api:write", "delete": "order:api:write"}}
	descriptor, err := crud.Describe(options, true)
	if err != nil {
		t.Fatal(err)
	}
	scopeCalls := 0
	authorizer, err := auth.Compile(auth.Declaration{Descriptor: descriptor, ResourceAccess: auth.ResourceAccessData, Rules: []auth.Rule{{Scope: &auth.ScopeRule{Resolver: orderDepartmentScope{calls: &scopeCalls}}}}}, auth.CompileOptions{Metadata: repo.ExecutionDescriptor().Metadata(), Checker: auth.ActionCheckerFunc(func(ctx context.Context, _ string) error { _, err := auth.RequirePrincipal(ctx); return err })})
	if err != nil {
		t.Fatal(err)
	}
	options.Authorizer = authorizer
	application, err := crud.NewApplication(repo, options)
	if err != nil {
		t.Fatal(err)
	}
	count, err := application.Count(ctx)
	if err != nil || count != 1 {
		t.Fatalf("Count=%d err=%v", count, err)
	}
	if _, err := application.Get(ctx, 2); !errors.Is(err, errors.NotFound) {
		t.Fatalf("outside read=%v", err)
	}
	called := false
	if _, err := application.Execute(ctx, "update", 2, func(context.Context, *declaredOrder) error { called = true; return nil }); !errors.Is(err, errors.NotFound) || called {
		t.Fatalf("outside command=%v called=%t", err, called)
	}
	beforeCalls := scopeCalls
	if _, err := application.ExecuteBulk(ctx, "update", []int64{1, 2}, func(_ context.Context, order *declaredOrder) error { order.Name = "changed"; return nil }); !errors.Is(err, errors.NotFound) {
		t.Fatalf("bulk=%v", err)
	}
	if scopeCalls-beforeCalls != 1 {
		t.Fatalf("bulk authorization count=%d", scopeCalls-beforeCalls)
	}
	stored, err := application.Get(ctx, 1)
	if err != nil || stored.Name != "inside" {
		t.Fatalf("rollback=%+v err=%v", stored, err)
	}
	if _, err := application.Execute(context.Background(), "update", 1, func(context.Context, *declaredOrder) error { called = true; return nil }); !errors.Is(err, errors.Unauthorized) || called {
		t.Fatalf("anonymous command=%v called=%t", err, called)
	}
}

func TestApplicationRejectsActionOnlyAndDescriptorDrift(t *testing.T) {
	repo, err := testkit.NewMemoryRepository[*declaredOrder, int64](nil)
	if err != nil {
		t.Fatal(err)
	}
	options := crud.Options[*declaredOrder, int64]{Resource: "order"}
	descriptor, err := crud.Describe(options, true)
	if err != nil {
		t.Fatal(err)
	}
	checker := auth.ActionCheckerFunc(func(context.Context, string) error { return nil })
	a, err := auth.Compile(auth.Declaration{Descriptor: descriptor, ResourceAccess: auth.ResourceAccessNone}, auth.CompileOptions{Checker: checker})
	if err != nil {
		t.Fatal(err)
	}
	options.Authorizer = a
	if _, err := crud.NewApplication(repo, options); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("action-only application=%v", err)
	}
	a, err = auth.Compile(auth.Declaration{Descriptor: descriptor, ResourceAccess: auth.ResourceAccessData}, auth.CompileOptions{Checker: checker, Metadata: repo.ExecutionDescriptor().Metadata()})
	if err != nil {
		t.Fatal(err)
	}
	options.Authorizer = a
	options.ActionPermissions = map[string]string{"read": "order:api:other"}
	if _, err := crud.NewApplication(repo, options); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("descriptor drift=%v", err)
	}
}
