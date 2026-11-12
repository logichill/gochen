package app_test

import (
	"context"
	"fmt"
	"testing"

	"gochen/app"
	"gochen/auth"
	domcrud "gochen/domain/crud"
	"gochen/domain/meta"
	"gochen/domain/spec"
	"gochen/errors"
	"gochen/testkit"
)

type commandEntity struct {
	domcrud.Entity[int64]
	Department int64 `dim:"department"`
	Name       string
}
type commandInput struct{ OrderID, ProductID int64 }
type commandAccess struct {
	order, product *app.CommandRepository[*commandEntity, int64]
}
type commandScope struct{}

func (commandScope) ResolveSpec(context.Context, auth.Request) (spec.Spec, []auth.Assignment, error) {
	return spec.Eq(meta.ByDimension("department"), int64(10)), nil, nil
}
func (commandScope) Vocabulary() auth.Vocabulary {
	return auth.Vocabulary{Refs: []meta.Ref{meta.ByDimension("department")}}
}

func TestCommandAuthorizesEveryResourceBeforeBusinessSideEffects(t *testing.T) {
	ctx, err := auth.WithPrincipal(t.Context(), auth.Principal{SubjectID: "member"})
	if err != nil {
		t.Fatal(err)
	}
	orderRepo, err := testkit.NewMemoryRepository[*commandEntity, int64](nil)
	if err != nil {
		t.Fatal(err)
	}
	productRepo, err := testkit.NewMemoryRepository[*commandEntity, int64](nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, repo := range []*testkit.MemoryRepository[*commandEntity, int64]{orderRepo, productRepo} {
		if err := repo.Create(ctx, &commandEntity{Entity: domcrud.Entity[int64]{ID: 1}, Department: 10, Name: "original"}, spec.All()); err != nil {
			t.Fatal(err)
		}
	}
	permissionErr := error(nil)
	checks := 0
	compile := func(resource string, repo *testkit.MemoryRepository[*commandEntity, int64]) *auth.Authorizer {
		t.Helper()
		a, err := auth.Compile(auth.Declaration{Descriptor: auth.Descriptor{Resource: resource, Actions: []auth.Action{{Name: "change", Kind: auth.KindUpdate, Permission: resource + ":api:write"}}}, ResourceAccess: auth.ResourceAccessData, Rules: []auth.Rule{{Scope: &auth.ScopeRule{Resolver: commandScope{}}}}}, auth.CompileOptions{Metadata: repo.ExecutionDescriptor().Metadata(), Checker: auth.ActionCheckerFunc(func(_ context.Context, permission string) error {
			checks++
			if permission == "product:api:write" {
				return permissionErr
			}
			return nil
		})})
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	orderAuth, productAuth := compile("order", orderRepo), compile("product", productRepo)
	assembled, sideEffects := 0, 0
	var retained *app.CommandRepository[*commandEntity, int64]
	command, err := app.NewCommand(app.CommandOptions[commandInput, commandAccess, struct{}]{
		Resources: []app.CommandResource[commandInput]{
			{Name: "order", Repository: orderRepo, Authorizer: orderAuth, Action: "change", Intent: func(input commandInput) (auth.Request, error) {
				return auth.Request{IDs: []string{fmt.Sprint(input.OrderID)}}, nil
			}},
			{Name: "product", Repository: productRepo, Authorizer: productAuth, Action: "change", Intent: func(input commandInput) (auth.Request, error) {
				return auth.Request{IDs: []string{fmt.Sprint(input.ProductID)}}, nil
			}},
		},
		Access: func(_ context.Context, _ commandInput, grants []*app.CommandGrant) (commandAccess, error) {
			assembled++
			order, err := app.NewCommandRepository(orderRepo, grants[0])
			if err != nil {
				return commandAccess{}, err
			}
			product, err := app.NewCommandRepository(productRepo, grants[1])
			return commandAccess{order: order, product: product}, err
		},
		Handler: func(ctx context.Context, _ commandInput, access commandAccess) (struct{}, error) {
			order, err := access.order.Get(ctx, 1)
			if err != nil {
				return struct{}{}, err
			}
			product, err := access.product.Get(ctx, 1)
			if err != nil {
				return struct{}{}, err
			}
			order.Name = "changed"
			product.Name = "changed"
			if err := access.order.Update(ctx, order); err != nil {
				return struct{}{}, err
			}
			if err := access.product.Update(ctx, product); err != nil {
				return struct{}{}, err
			}
			sideEffects++
			retained = access.order
			return struct{}{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := command.AuthorizationBinding()
	if err != nil {
		t.Fatal(err)
	}
	binding.Entry = auth.Entry{Kind: "command", Key: "order.change"}
	if report, err := auth.ValidateEntries([]auth.Entry{binding.Entry}, []auth.EntryBinding{binding}); err != nil || len(report) != 1 || len(report[0].Resources) != 1 {
		t.Fatalf("command entry=%+v err=%v", report, err)
	}
	permissionErr = errors.NewCode(errors.Forbidden, "product denied")
	if _, err := command.Execute(ctx, commandInput{OrderID: 1, ProductID: 1}); !errors.Is(err, errors.Forbidden) || checks != 2 || assembled != 0 || sideEffects != 0 {
		t.Fatalf("denial=%v checks=%d assembled=%d effects=%d", err, checks, assembled, sideEffects)
	}
	stored, err := orderRepo.Get(ctx, 1, spec.All())
	if err != nil || stored.Name != "original" {
		t.Fatalf("denied command wrote order: %+v %v", stored, err)
	}
	permissionErr = nil
	if _, err := command.Execute(ctx, commandInput{OrderID: 1, ProductID: 1}); err != nil || checks != 4 || assembled != 1 || sideEffects != 1 {
		t.Fatalf("execution=%v checks=%d assembled=%d effects=%d", err, checks, assembled, sideEffects)
	}
	if _, err := retained.Get(ctx, 1); !errors.Is(err, errors.FailedPrecondition) {
		t.Fatalf("escaped access=%v", err)
	}
	stored, err = productRepo.Get(ctx, 1, spec.All())
	if err != nil || stored.Name != "changed" {
		t.Fatalf("product=%+v err=%v", stored, err)
	}
}

func TestCommandRepositoryProtectsIntentVersionAndIdentity(t *testing.T) {
	ctx, err := auth.WithPrincipal(t.Context(), auth.Principal{SubjectID: "member"})
	if err != nil {
		t.Fatal(err)
	}
	repo, err := testkit.NewMemoryRepository[*commandEntity, int64](nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, &commandEntity{Entity: domcrud.Entity[int64]{ID: 1}, Department: 10}, spec.All()); err != nil {
		t.Fatal(err)
	}
	declaration := auth.Declaration{Descriptor: auth.Descriptor{Resource: "order", Actions: []auth.Action{{Name: "change", Kind: auth.KindUpdate}}}, ResourceAccess: auth.ResourceAccessData, Rules: []auth.Rule{{Scope: &auth.ScopeRule{Resolver: commandScope{}}}}}
	a, err := auth.Compile(declaration, auth.CompileOptions{Metadata: repo.ExecutionDescriptor().Metadata(), Checker: auth.ActionCheckerFunc(func(context.Context, string) error { return nil })})
	if err != nil {
		t.Fatal(err)
	}
	command, err := app.NewCommand(app.CommandOptions[int64, *app.CommandRepository[*commandEntity, int64], struct{}]{
		Resources: []app.CommandResource[int64]{{Name: "order", Repository: repo, Authorizer: a, Action: "change", Intent: func(int64) (auth.Request, error) { return auth.Request{IDs: []string{"1"}}, nil }}},
		Access: func(_ context.Context, _ int64, grants []*app.CommandGrant) (*app.CommandRepository[*commandEntity, int64], error) {
			return app.NewCommandRepository(repo, grants[0])
		},
		Handler: func(ctx context.Context, _ int64, access *app.CommandRepository[*commandEntity, int64]) (struct{}, error) {
			if _, err := access.Get(ctx, 2); !errors.Is(err, errors.Forbidden) {
				t.Fatalf("outside intent=%v", err)
			}
			entity, err := access.Get(ctx, 1)
			if err != nil {
				return struct{}{}, err
			}
			entity.Department = 20
			if err := access.Update(ctx, entity); !errors.Is(err, errors.Forbidden) {
				t.Fatalf("protected field=%v", err)
			}
			entity.Department = 10
			entity.Version++
			if err := access.Update(ctx, entity); !errors.Is(err, errors.Concurrency) {
				t.Fatalf("version=%v", err)
			}
			other, err := auth.WithPrincipal(ctx, auth.Principal{SubjectID: "other"})
			if err != nil {
				return struct{}{}, err
			}
			back, err := auth.WithPrincipal(other, auth.Principal{SubjectID: "member"})
			if err != nil {
				return struct{}{}, err
			}
			if _, err := access.Get(back, 1); !errors.Is(err, errors.Forbidden) {
				t.Fatalf("identity replay=%v", err)
			}
			return struct{}{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := command.Execute(ctx, 1); err != nil {
		t.Fatal(err)
	}
}

func TestCommandRejectsFactOutsideIntentAndEmptyWriteScopeBeforeEffects(t *testing.T) {
	repo, err := testkit.NewMemoryRepository[*commandEntity, int64](nil)
	if err != nil {
		t.Fatal(err)
	}
	fact, err := auth.NewFact("trusted entity", func(context.Context, auth.Request) (*commandEntity, error) {
		return &commandEntity{Entity: domcrud.Entity[int64]{ID: 2}, Department: 10}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, empty := range []bool{false, true} {
		t.Run(fmt.Sprint("empty scope=", empty), func(t *testing.T) {
			policy, err := auth.WithFacts(auth.PolicyFunc(func(context.Context, auth.Request) (auth.Decision, error) {
				condition := spec.All()
				if empty {
					condition = spec.Spec{}
				}
				return auth.Decision{Allowed: true, Spec: condition}, nil
			}), fact)
			if err != nil {
				t.Fatal(err)
			}
			a, err := auth.Compile(auth.Declaration{Descriptor: auth.Descriptor{Resource: "order", Actions: []auth.Action{{Name: "change", Kind: auth.KindUpdate}}}, ResourceAccess: auth.ResourceAccessData, Rules: []auth.Rule{{Constraints: []auth.IPolicy{policy}}}}, auth.CompileOptions{Metadata: repo.ExecutionDescriptor().Metadata(), Checker: auth.ActionCheckerFunc(func(context.Context, string) error { return nil })})
			if err != nil {
				t.Fatal(err)
			}
			assembled, effects := 0, 0
			command, err := app.NewCommand(app.CommandOptions[int64, *app.CommandRepository[*commandEntity, int64], struct{}]{
				Resources: []app.CommandResource[int64]{{Name: "order", Repository: repo, Authorizer: a, Action: "change", Intent: func(id int64) (auth.Request, error) { return auth.Request{IDs: []string{fmt.Sprint(id)}}, nil }}},
				Access: func(ctx context.Context, _ int64, grants []*app.CommandGrant) (*app.CommandRepository[*commandEntity, int64], error) {
					assembled++
					access, err := app.NewCommandRepository(repo, grants[0])
					if err != nil {
						return nil, err
					}
					_, err = access.FromFact(ctx, fact)
					return access, err
				},
				Handler: func(context.Context, int64, *app.CommandRepository[*commandEntity, int64]) (struct{}, error) {
					effects++
					return struct{}{}, nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := command.Execute(t.Context(), 1); !errors.Is(err, errors.Forbidden) || effects != 0 {
				t.Fatalf("intent denial=%v effects=%d", err, effects)
			}
			if empty && assembled != 0 {
				t.Fatal("empty write scope reached access assembly")
			}
		})
	}
}

func TestCommandRejectsUninitializedReceiver(t *testing.T) {
	var missing *app.Command[struct{}, struct{}, struct{}]
	for _, command := range []*app.Command[struct{}, struct{}, struct{}]{missing, new(app.Command[struct{}, struct{}, struct{}])} {
		if _, err := command.Execute(t.Context(), struct{}{}); !errors.Is(err, errors.InvalidInput) {
			t.Fatalf("uninitialized execution must return InvalidInput: %v", err)
		}
		if _, err := command.AuthorizationBinding(); !errors.Is(err, errors.InvalidInput) {
			t.Fatalf("uninitialized metadata must return InvalidInput: %v", err)
		}
	}
}
