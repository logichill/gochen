package crud

import (
	"context"
	"testing"

	"gochen/app/internal/execution"
	"gochen/errors"
)

func TestDeleteBulkEmptyBulkSkipsValidationAndAuthorization(t *testing.T) {
	authorizer := newCountingAuthorizer()
	app, _ := newTestApplication(t, Options[*testEntity, int64]{
		Authorizer: authorizer,
		Limits:     execution.Limits{MaxBulkSize: 2},
	})
	ctx := context.Background()

	if err := app.DeleteBulk(ctx, nil); err != nil {
		t.Fatalf("DeleteBulk(empty) = %v", err)
	}
	if err := app.DeleteBulk(ctx, []int64{1, 1}); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("DeleteBulk(duplicate) = %v, want InvalidInput", err)
	}
	if err := app.DeleteBulk(ctx, []int64{1, 2, 3}); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("DeleteBulk(over limit) = %v, want InvalidInput", err)
	}
	if len(authorizer.calls) != 0 {
		t.Fatalf("rejected bulk must not authorize, calls = %+v", authorizer.calls)
	}
}

func TestExecuteBulkRunsHooksAndValidationOncePerEntity(t *testing.T) {
	var before, after int
	validator := &trackingValidator{}
	app, _ := newTestApplication(t, Options[*testEntity, int64]{
		Validator: validator,
		Hooks: Hooks[*testEntity]{
			ActionUpdate: Stage[*testEntity]{
				Before: func(context.Context, *testEntity) error {
					before++
					return nil
				},
				After: func(context.Context, *testEntity) error {
					after++
					return nil
				},
			},
		},
	})
	ctx := context.Background()
	first := mustCreate(t, app, ctx, "a")
	second := mustCreate(t, app, ctx, "b")
	validator.called = 0

	if _, err := app.ExecuteBulk(ctx, ActionUpdate, []int64{first.GetID(), second.GetID()}, func(_ context.Context, entity *testEntity) error {
		entity.Active = true
		return nil
	}); err != nil {
		t.Fatalf("ExecuteBulk(): %v", err)
	}
	if before != 2 || after != 2 || validator.called != 2 {
		t.Fatalf("before=%d after=%d validate=%d, want 2 each", before, after, validator.called)
	}
}

func TestExecuteBulkAfterHookFailureRollsBackWholeBulk(t *testing.T) {
	want := errors.New("after update failed")
	app, _ := newTestApplication(t, Options[*testEntity, int64]{
		Hooks: Hooks[*testEntity]{
			ActionUpdate: Stage[*testEntity]{
				After: func(_ context.Context, entity *testEntity) error {
					if entity.Name == "second-changed" {
						return want
					}
					return nil
				},
			},
		},
	})
	ctx := context.Background()
	first := mustCreate(t, app, ctx, "first")
	second := mustCreate(t, app, ctx, "second")

	_, err := app.ExecuteBulk(ctx, ActionUpdate, []int64{first.GetID(), second.GetID()}, func(_ context.Context, entity *testEntity) error {
		entity.Name += "-changed"
		return nil
	})
	if !errors.Is(err, want) {
		t.Fatalf("ExecuteBulk() = %v, want hook error", err)
	}
	wantNames := map[int64]string{first.GetID(): "first", second.GetID(): "second"}
	for id, name := range wantNames {
		stored, err := app.Get(ctx, id)
		if err != nil {
			t.Fatalf("Get(%d): %v", id, err)
		}
		if stored.Name != name {
			t.Fatalf("bulk rollback failed, stored name = %q, want %q", stored.Name, name)
		}
		if stored.GetVersion() != 0 {
			t.Fatalf("rollback advanced version to %d", stored.GetVersion())
		}
	}
}

func TestDeleteBulkRunsDeleteHookPerEntity(t *testing.T) {
	var hookIDs []int64
	app, _ := newTestApplication(t, Options[*testEntity, int64]{
		Hooks: Hooks[*testEntity]{
			ActionDelete: Stage[*testEntity]{
				Before: func(_ context.Context, entity *testEntity) error {
					hookIDs = append(hookIDs, entity.GetID())
					return nil
				},
			},
		},
	})
	ctx := context.Background()
	a := mustCreate(t, app, ctx, "a")
	b := mustCreate(t, app, ctx, "b")

	if err := app.DeleteBulk(ctx, []int64{a.GetID(), b.GetID()}); err != nil {
		t.Fatalf("DeleteBulk(): %v", err)
	}
	if len(hookIDs) != 2 || hookIDs[0] != a.GetID() || hookIDs[1] != b.GetID() {
		t.Fatalf("delete hook ids = %v", hookIDs)
	}
}

func TestExecuteNewBulkPostCommitRunsAfterCommit(t *testing.T) {
	var app *Application[*testEntity, int64]
	var committed int
	ctx := context.Background()
	app, _ = newTestApplication(t, Options[*testEntity, int64]{
		Hooks: Hooks[*testEntity]{
			ActionCreate: Stage[*testEntity]{
				PostCommit: func(_ context.Context, entity *testEntity) error {
					if _, err := app.Get(ctx, entity.GetID()); err != nil {
						return err
					}
					committed++
					return nil
				},
			},
		},
	})

	created, err := app.ExecuteNewBulk(ctx, ActionCreate, func(context.Context) ([]*testEntity, error) {
		return []*testEntity{{Name: "a"}, {Name: "b"}}, nil
	})
	if err != nil {
		t.Fatalf("ExecuteNewBulk(): %v", err)
	}
	if len(created) != 2 || committed != 2 {
		t.Fatalf("created=%d committed=%d, want 2 each", len(created), committed)
	}
}
