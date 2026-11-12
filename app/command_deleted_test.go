package app_test

import (
	"context"
	"fmt"

	"testing"
	"time"

	"gochen/app"
	"gochen/auth"

	domcrud "gochen/domain/crud"
	"gochen/domain/spec"
	"gochen/errors"
	"gochen/testkit"
)

type deletedCommandEntity struct {
	domcrud.TrackedEntity[int64]
	Department int64 `dim:"department"`
}

func TestCommandDeletedLifecyclePreservesScopeVersionAndRevocation(t *testing.T) {
	repository, err := testkit.NewAuditedMemoryRepository[*deletedCommandEntity, int64](nil)
	if err != nil {
		t.Fatal(err)
	}
	for id, department := range map[int64]int64{1: 10, 2: 20} {
		entity := &deletedCommandEntity{TrackedEntity: domcrud.TrackedEntity[int64]{Entity: domcrud.Entity[int64]{ID: id}}, Department: department}
		if err := repository.Create(t.Context(), entity, spec.All()); err != nil {
			t.Fatal(err)
		}
		if err := entity.SoftDelete(time.Now()); err != nil {
			t.Fatal(err)
		}
		if err := repository.Update(t.Context(), entity, spec.All()); err != nil {
			t.Fatal(err)
		}
	}
	type input struct {
		ID   int64
		Mode string
	}
	var retained *app.CommandRepository[*deletedCommandEntity, int64]
	assemble := func(name string, kind auth.Kind) *app.Command[input, *app.CommandRepository[*deletedCommandEntity, int64], *deletedCommandEntity] {
		t.Helper()
		authorizer, err := auth.Compile(auth.Declaration{
			Descriptor:     auth.Descriptor{Resource: "document", Actions: []auth.Action{{Name: name, Kind: kind, Permission: "document:api:write"}}},
			ResourceAccess: auth.ResourceAccessData,
			Rules:          []auth.Rule{{Scope: &auth.ScopeRule{Resolver: commandScope{}}}},
		}, auth.CompileOptions{Metadata: repository.ExecutionDescriptor().Metadata(), Checker: auth.ActionCheckerFunc(func(context.Context, string) error { return nil })})
		if err != nil {
			t.Fatal(err)
		}
		command, err := app.NewCommand(app.CommandOptions[input, *app.CommandRepository[*deletedCommandEntity, int64], *deletedCommandEntity]{
			Resources: []app.CommandResource[input]{{Name: "document", Repository: repository, Authorizer: authorizer, Action: name, Intent: func(in input) (auth.Request, error) { return auth.Request{IDs: []string{fmt.Sprint(in.ID)}}, nil }}},
			Access: func(_ context.Context, _ input, grants []*app.CommandGrant) (*app.CommandRepository[*deletedCommandEntity, int64], error) {
				return app.NewCommandRepository(repository, grants[0])
			},
			Handler: func(ctx context.Context, in input, access *app.CommandRepository[*deletedCommandEntity, int64]) (*deletedCommandEntity, error) {
				retained = access
				id := in.ID
				if in.Mode == "other_id" {
					id++
				}
				entity, err := access.GetWithDeleted(ctx, id)
				if err != nil {
					return nil, err
				}
				if in.Mode == "stale" {
					entity.Version++
				}
				if in.Mode == "move_scope" {
					entity.Department = 20
				}
				if name == "purge" {
					return entity, access.Purge(ctx, entity)
				}
				return entity, access.Restore(ctx, entity)
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		return command
	}
	restore := assemble("restore", auth.KindUpdate)
	for _, test := range []struct {
		input input
		code  errors.ErrorCode
	}{
		{input{ID: 2}, errors.NotFound},
		{input{ID: 1, Mode: "stale"}, errors.Concurrency},
		{input{ID: 1, Mode: "move_scope"}, errors.Forbidden},
		{input{ID: 1, Mode: "other_id"}, errors.Forbidden},
	} {
		if _, err := restore.Execute(t.Context(), test.input); !errors.Is(err, test.code) {
			t.Fatalf("restore %v: %v", test.input, err)
		}
	}
	before, err := repository.GetWithDeleted(t.Context(), 1, spec.All())
	if err != nil || !before.IsDeleted() {
		t.Fatalf("rejected restore changed storage: %v %v", before, err)
	}
	restored, err := restore.Execute(t.Context(), input{ID: 1})
	if err != nil || restored.IsDeleted() || restored.Version != before.Version+1 {
		t.Fatalf("restored=%+v err=%v", restored, err)
	}
	if _, err := retained.GetWithDeleted(t.Context(), 1); !errors.Is(err, errors.FailedPrecondition) {
		t.Fatalf("retained deleted reader=%v", err)
	}
	if err := retained.Restore(t.Context(), restored); !errors.Is(err, errors.FailedPrecondition) {
		t.Fatalf("retained restore=%v", err)
	}
	purge := assemble("purge", auth.KindDelete)
	if _, err := purge.Execute(t.Context(), input{ID: 2}); !errors.Is(err, errors.NotFound) {
		t.Fatalf("outside scope purge=%v", err)
	}
	if _, err := purge.Execute(t.Context(), input{ID: 1, Mode: "stale"}); !errors.Is(err, errors.Concurrency) {
		t.Fatalf("stale purge=%v", err)
	}
	if _, err := purge.Execute(t.Context(), input{ID: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.GetWithDeleted(t.Context(), 1, spec.All()); !errors.Is(err, errors.NotFound) {
		t.Fatalf("purge left entity: %v", err)
	}
	if err := retained.Purge(t.Context(), restored); !errors.Is(err, errors.FailedPrecondition) {
		t.Fatalf("retained purge=%v", err)
	}
}
