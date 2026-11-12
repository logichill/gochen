package app_test

import (
	"context"
	"testing"

	"gochen/app"
	"gochen/app/crud"
	"gochen/auth"
	"gochen/contextx"
	domcrud "gochen/domain/crud"
	"gochen/domain/meta"
	"gochen/domain/spec"
	"gochen/errors"
	"gochen/testkit"
)

func TestCommandExecutionReusesDecisionAndRetainsGuards(t *testing.T) {
	ctx, err := auth.WithPrincipal(t.Context(), auth.Principal{SubjectID: "member"})
	if err != nil {
		t.Fatal(err)
	}
	repo, err := testkit.NewMemoryRepository[*commandEntity, int64](nil)
	if err != nil {
		t.Fatal(err)
	}
	other, err := testkit.NewMemoryRepository[*commandEntity, int64](nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []*commandEntity{{Entity: domcrud.Entity[int64]{ID: 1}, Department: 10, Name: "before"}, {Entity: domcrud.Entity[int64]{ID: 2}, Department: 20, Name: "outside"}, {Entity: domcrud.Entity[int64]{ID: 3}, Department: 20, Name: "outside-scope"}} {
		if err := repo.Create(ctx, item, spec.All()); err != nil {
			t.Fatal(err)
		}
	}
	options := crud.Options[*commandEntity, int64]{Resource: "order"}
	descriptor, err := crud.Describe(options, true)
	if err != nil {
		t.Fatal(err)
	}
	checks := 0
	compile := func() *auth.Authorizer {
		a, err := auth.Compile(auth.Declaration{Descriptor: descriptor, ResourceAccess: auth.ResourceAccessData, Rules: []auth.Rule{{Scope: &auth.ScopeRule{Resolver: commandScope{}}}}}, auth.CompileOptions{Metadata: repo.ExecutionDescriptor().Metadata(), Checker: auth.ActionCheckerFunc(func(context.Context, string) error { checks++; return nil })})
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	authorizer, another := compile(), compile()
	options.Authorizer = authorizer
	var retained *crud.Application[*commandEntity, int64]
	command, err := app.NewCommand(app.CommandOptions[int64, *app.CommandGrant, struct{}]{
		Resources: []app.CommandResource[int64]{{Name: "order", Repository: repo, Authorizer: authorizer, Action: "update", Intent: func(int64) (auth.Request, error) { return auth.Request{IDs: []string{"1", "3"}}, nil }}},
		Access: func(_ context.Context, _ int64, grants []*app.CommandGrant) (*app.CommandGrant, error) {
			return grants[0], nil
		},
		Handler: func(ctx context.Context, _ int64, grant *app.CommandGrant) (struct{}, error) {
			opts := options
			opts.CommandGrant = grant
			if _, err := crud.NewApplication(other, opts); !errors.Is(err, errors.InvalidInput) {
				t.Fatalf("changed repository=%v", err)
			}
			opts.Authorizer = another
			if _, err := crud.NewApplication(repo, opts); !errors.Is(err, errors.InvalidInput) {
				t.Fatalf("changed authorizer=%v", err)
			}
			opts.Authorizer = authorizer
			inner, err := crud.NewApplication(repo, opts)
			if err != nil {
				return struct{}{}, err
			}
			retained = inner
			if _, err := inner.Execute(ctx, "update", 2, func(context.Context, *commandEntity) error { t.Fatal("outside intent reached handler"); return nil }); !errors.Is(err, errors.Forbidden) {
				t.Fatalf("outside intent=%v", err)
			}
			if _, err := inner.Get(ctx, 1); !errors.Is(err, errors.Forbidden) {
				t.Fatalf("changed action=%v", err)
			}
			if _, err := inner.Execute(ctx, "update", 3, func(context.Context, *commandEntity) error { t.Fatal("outside scope reached handler"); return nil }); !errors.Is(err, errors.NotFound) {
				t.Fatalf("scope condition=%v", err)
			}
			changed, err := auth.WithPrincipal(ctx, auth.Principal{SubjectID: "other"})
			if err != nil {
				t.Fatal(err)
			}
			if err := inner.Update(changed, &commandEntity{Entity: domcrud.Entity[int64]{ID: 1}, Department: 10}); !errors.Is(err, errors.Forbidden) {
				t.Fatalf("changed principal=%v", err)
			}
			changed, err = contextx.WithTenantID(ctx, "other")
			if err != nil {
				t.Fatal(err)
			}
			if err := inner.Update(changed, &commandEntity{Entity: domcrud.Entity[int64]{ID: 1}, Department: 10}); !errors.Is(err, errors.Forbidden) {
				t.Fatalf("changed tenant=%v", err)
			}
			_, err = inner.Execute(ctx, "update", 1, func(_ context.Context, item *commandEntity) error { item.Name = "after"; return nil })
			if err != nil {
				return struct{}{}, err
			}
			if err := inner.Update(ctx, &commandEntity{Entity: domcrud.Entity[int64]{ID: 1}, Department: 10, Name: "stale"}); !errors.Is(err, errors.Concurrency) {
				t.Fatalf("stale version=%v", err)
			}
			return struct{}{}, nil
		}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := command.Execute(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if checks != 1 {
		t.Fatalf("authorization checks=%d, want 1", checks)
	}
	if err := retained.Update(ctx, &commandEntity{Entity: domcrud.Entity[int64]{ID: 1}, Department: 10}); !errors.Is(err, errors.FailedPrecondition) {
		t.Fatalf("escaped execution=%v", err)
	}
	stored, err := repo.Get(ctx, 1, spec.All())
	if err != nil || stored.Name != "after" || stored.Version != 1 {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
}

type commandCreateScope struct{}

func (commandCreateScope) Vocabulary() auth.Vocabulary {
	return auth.Vocabulary{Refs: []meta.Ref{meta.ByDimension("department")}, Assignable: []meta.Dimension{"department"}}
}
func (commandCreateScope) ResolveSpec(context.Context, auth.Request) (spec.Spec, []auth.Assignment, error) {
	return spec.Eq(meta.ByDimension("department"), int64(10)), []auth.Assignment{{Dimension: "department", Value: int64(10)}}, nil
}

func TestCommandExecutionPreservesCreateAssignments(t *testing.T) {
	repo, err := testkit.NewMemoryRepository[*commandEntity, int64](nil, testkit.WithMemoryResourceContract[*commandEntity, int64](app.ResourceContract{Assignable: []meta.Dimension{"department"}}))
	if err != nil {
		t.Fatal(err)
	}
	options := crud.Options[*commandEntity, int64]{Resource: "order"}
	descriptor, err := crud.Describe(options, true)
	if err != nil {
		t.Fatal(err)
	}
	checks := 0
	authorizer, err := auth.Compile(auth.Declaration{Descriptor: descriptor, ResourceAccess: auth.ResourceAccessData, Rules: []auth.Rule{{Scope: &auth.ScopeRule{Resolver: commandCreateScope{}}}}}, auth.CompileOptions{Metadata: repo.ExecutionDescriptor().Metadata(), Checker: auth.ActionCheckerFunc(func(context.Context, string) error { checks++; return nil })})
	if err != nil {
		t.Fatal(err)
	}
	options.Authorizer = authorizer
	ctx, err := auth.WithPrincipal(t.Context(), auth.Principal{SubjectID: "member"})
	if err != nil {
		t.Fatal(err)
	}
	command, err := app.NewCommand(app.CommandOptions[struct{}, *app.CommandGrant, *commandEntity]{Resources: []app.CommandResource[struct{}]{{Name: "order", Repository: repo, Authorizer: authorizer, Action: "create", Intent: func(struct{}) (auth.Request, error) { return auth.Request{}, nil }}},
		Access: func(_ context.Context, _ struct{}, grants []*app.CommandGrant) (*app.CommandGrant, error) {
			return grants[0], nil
		},
		Handler: func(ctx context.Context, _ struct{}, grant *app.CommandGrant) (*commandEntity, error) {
			decision, err := grant.ExecutionDecision(ctx, "order", "create", nil)
			if err != nil {
				return nil, err
			}
			decision.Assign[0].Value = int64(20)
			opts := options
			opts.CommandGrant = grant
			inner, err := crud.NewApplication(repo, opts)
			if err != nil {
				return nil, err
			}
			item := &commandEntity{Entity: domcrud.Entity[int64]{ID: 1}, Name: "assigned"}
			return item, inner.Create(ctx, item)
		}})
	if err != nil {
		t.Fatal(err)
	}
	item, err := command.Execute(ctx, struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	if checks != 1 || item.Department != 10 {
		t.Fatalf("checks=%d item=%+v", checks, item)
	}
}
