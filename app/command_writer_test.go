package app_test

import (
	"context"
	"fmt"
	"testing"

	"gochen/app"
	"gochen/auth"
	domcrud "gochen/domain/crud"
	"gochen/domain/spec"
	"gochen/errors"
	"gochen/testkit"
)

func TestCommandWriterLimitsIDsAndScopeAndRevokesCapability(t *testing.T) {
	repository, err := testkit.NewMemoryRepository[*commandEntity, int64](nil)
	if err != nil {
		t.Fatal(err)
	}
	for id, department := range map[int64]int64{1: 10, 2: 20} {
		if err := repository.Create(t.Context(), &commandEntity{Entity: domcrud.Entity[int64]{ID: id}, Department: department, Name: "original"}, spec.All()); err != nil {
			t.Fatal(err)
		}
	}
	denied := false
	authorizer, err := auth.Compile(auth.Declaration{Descriptor: auth.Descriptor{Resource: "document", Actions: []auth.Action{{Name: "change", Kind: auth.KindUpdate, Permission: "document:api:write"}}}, ResourceAccess: auth.ResourceAccessData, Rules: []auth.Rule{{Scope: &auth.ScopeRule{Resolver: commandScope{}}}}}, auth.CompileOptions{Metadata: repository.ExecutionDescriptor().Metadata(), Checker: auth.ActionCheckerFunc(func(context.Context, string) error {
		if denied {
			return errors.NewCode(errors.Forbidden, "denied")
		}
		return nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	var retained *app.CommandWriter[int64, []string]
	calls := 0
	failure := errors.New("storage failed after one write")
	command, err := app.NewCommand(app.CommandOptions[int64, *app.CommandWriter[int64, []string], []string]{Resources: []app.CommandResource[int64]{{Name: "document", Repository: repository, Authorizer: authorizer, Action: "change", Intent: func(id int64) (auth.Request, error) { return auth.Request{IDs: []string{fmt.Sprint(id)}}, nil }}}, Access: func(_ context.Context, _ int64, grants []*app.CommandGrant) (*app.CommandWriter[int64, []string], error) {
		return app.NewCommandWriter(repository, grants[0], func(ctx context.Context, id int64, c spec.Spec) ([]string, error) {
			calls++
			entity, err := repository.Get(ctx, id, c)
			if err != nil {
				return nil, err
			}
			entity.Name = "changed"
			if err := repository.Update(ctx, entity, c); err != nil {
				return nil, err
			}
			return []string{"changed"}, failure
		})
	}, Handler: func(ctx context.Context, id int64, w *app.CommandWriter[int64, []string]) ([]string, error) {
		retained = w
		return w.Write(ctx, id)
	}})
	if err != nil {
		t.Fatal(err)
	}
	if result, err := command.Execute(t.Context(), int64(2)); !errors.Is(err, errors.NotFound) || result != nil {
		t.Fatalf("scope escape: %v %v", result, err)
	}
	if result, err := command.Execute(t.Context(), int64(1)); !errors.Is(err, failure) || len(result) != 1 {
		t.Fatalf("partial result: %v %v", result, err)
	}
	if _, err := retained.Write(t.Context(), int64(1)); !errors.Is(err, errors.FailedPrecondition) {
		t.Fatalf("retained writer: %v", err)
	}
	denied = true
	before := calls
	if result, err := command.Execute(t.Context(), int64(1)); !errors.Is(err, errors.Forbidden) || result != nil || calls != before {
		t.Fatalf("denial reached writer: result=%v error=%v calls=%d", result, err, calls)
	}
}
