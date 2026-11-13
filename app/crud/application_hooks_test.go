package crud

import (
	"context"
	"testing"

	"gochen/auth"
	"gochen/contextx"
	"gochen/errors"
)

func TestCreateAfterHookFailureRollsBack(t *testing.T) {
	want := errors.New("after create failed")
	app, _ := newTestApplication(t, Options[*testEntity, int64]{DeleteMode: DeletePurge,
		Hooks: Hooks[*testEntity]{
			ActionCreate: Stage[*testEntity]{
				After: func(context.Context, *testEntity) error { return want },
			},
		},
	})
	ctx := context.Background()
	entity := &testEntity{Name: "doomed"}
	if err := app.Create(ctx, entity); !errors.Is(err, want) {
		t.Fatalf("Create() = %v, want hook error", err)
	}
	if _, err := app.Get(ctx, entity.GetID()); !errors.Is(err, errors.NotFound) {
		t.Fatalf("Get() after rollback = %v, want NotFound", err)
	}
	count, err := app.Count(ctx)
	if err != nil || count != 0 {
		t.Fatalf("Count() = %d, %v", count, err)
	}
}

func TestUpdateAfterHookFailureKeepsStoredState(t *testing.T) {
	want := errors.New("after update failed")
	app, _ := newTestApplication(t, Options[*testEntity, int64]{DeleteMode: DeletePurge,
		Hooks: Hooks[*testEntity]{
			ActionUpdate: Stage[*testEntity]{
				After: func(context.Context, *testEntity) error { return want },
			},
		},
	})
	ctx := context.Background()
	created := mustCreate(t, app, ctx, "original")
	loaded, err := app.Get(ctx, created.GetID())
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}
	loaded.Name = "changed"

	if err := app.Update(ctx, loaded); !errors.Is(err, want) {
		t.Fatalf("Update() = %v, want hook error", err)
	}
	stored, err := app.Get(ctx, created.GetID())
	if err != nil {
		t.Fatalf("Get() after rollback: %v", err)
	}
	if stored.Name != "original" || stored.GetVersion() != 0 {
		t.Fatalf("stored after rollback = %+v", stored)
	}
}

func TestDeleteAfterHookFailureKeepsEntity(t *testing.T) {
	want := errors.New("after delete failed")
	app, _ := newTestApplication(t, Options[*testEntity, int64]{DeleteMode: DeletePurge,
		Hooks: Hooks[*testEntity]{
			ActionPurge: Stage[*testEntity]{
				After: func(context.Context, *testEntity) error { return want },
			},
		},
	})
	ctx := context.Background()
	created := mustCreate(t, app, ctx, "keep")

	if err := app.Delete(ctx, created.GetID()); !errors.Is(err, want) {
		t.Fatalf("Delete() = %v, want hook error", err)
	}
	stored, err := app.Get(ctx, created.GetID())
	if err != nil {
		t.Fatalf("Get() after rollback: %v", err)
	}
	if stored.Name != "keep" {
		t.Fatalf("stored after rollback = %+v", stored)
	}
}

func TestCreateAfterHookRunsBeforeCommitAndPostCommitAfterCommit(t *testing.T) {
	var app *Application[*testEntity, int64]
	var afterSawCommitted, postCommitSawCommitted bool
	ctx := context.Background()
	app, _ = newTestApplication(t, Options[*testEntity, int64]{DeleteMode: DeletePurge,
		Hooks: Hooks[*testEntity]{
			ActionCreate: Stage[*testEntity]{
				After: func(context.Context, *testEntity) error {
					// 外层 ctx 看不到未提交的事务状态。
					_, err := app.Get(ctx, 1)
					afterSawCommitted = err == nil
					return nil
				},
				PostCommit: func(context.Context, *testEntity) error {
					_, err := app.Get(ctx, 1)
					postCommitSawCommitted = err == nil
					return nil
				},
			},
		},
	})

	if err := app.Create(ctx, &testEntity{Name: "first"}); err != nil {
		t.Fatalf("Create(): %v", err)
	}
	if afterSawCommitted {
		t.Fatal("After hook must run before commit")
	}
	if !postCommitSawCommitted {
		t.Fatal("PostCommit hook must run after commit")
	}
}

func TestCreatePostCommitErrorIsReturnedAfterCommit(t *testing.T) {
	want := errors.New("post commit failed")
	app, _ := newTestApplication(t, Options[*testEntity, int64]{DeleteMode: DeletePurge,
		Hooks: Hooks[*testEntity]{
			ActionCreate: Stage[*testEntity]{
				PostCommit: func(context.Context, *testEntity) error { return want },
			},
		},
	})
	ctx := context.Background()
	entity := &testEntity{Name: "committed"}

	err := app.Create(ctx, entity)
	if !errors.Is(err, want) {
		t.Fatalf("Create() = %v, want post-commit error", err)
	}
	if !contextx.IsAfterCommitError(err) {
		t.Fatal("post-commit error must be marked as after-commit")
	}
	stored, err := app.Get(ctx, entity.GetID())
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}
	if stored.Name != "committed" {
		t.Fatalf("create must stay committed, stored = %+v", stored)
	}
}

func TestHooksAreKeyedByActionName(t *testing.T) {
	var createCalls, updateCalls, deleteCalls int
	record := func(counter *int) func(context.Context, *testEntity) error {
		return func(context.Context, *testEntity) error {
			*counter++
			return nil
		}
	}
	app, _ := newTestApplication(t, Options[*testEntity, int64]{DeleteMode: DeletePurge,
		Hooks: Hooks[*testEntity]{
			ActionCreate: Stage[*testEntity]{Before: record(&createCalls), After: record(&createCalls), PostCommit: record(&createCalls)},
			ActionUpdate: Stage[*testEntity]{Before: record(&updateCalls), After: record(&updateCalls), PostCommit: record(&updateCalls)},
			ActionPurge:  Stage[*testEntity]{Before: record(&deleteCalls), After: record(&deleteCalls), PostCommit: record(&deleteCalls)},
		},
	})
	ctx := context.Background()

	created := mustCreate(t, app, ctx, "a")
	if createCalls != 3 || updateCalls != 0 || deleteCalls != 0 {
		t.Fatalf("after create: create=%d update=%d delete=%d", createCalls, updateCalls, deleteCalls)
	}

	loaded, err := app.Get(ctx, created.GetID())
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}
	loaded.Name = "b"
	if err := app.Update(ctx, loaded); err != nil {
		t.Fatalf("Update(): %v", err)
	}
	if createCalls != 3 || updateCalls != 3 || deleteCalls != 0 {
		t.Fatalf("after update: create=%d update=%d delete=%d", createCalls, updateCalls, deleteCalls)
	}

	if err := app.Delete(ctx, created.GetID()); err != nil {
		t.Fatalf("Delete(): %v", err)
	}
	if createCalls != 3 || updateCalls != 3 || deleteCalls != 3 {
		t.Fatalf("after delete: create=%d update=%d delete=%d", createCalls, updateCalls, deleteCalls)
	}
}

func TestCustomActionUsesItsOwnHookKey(t *testing.T) {
	var createCalls, importCalls int
	app, _ := newTestApplication(t, Options[*testEntity, int64]{DeleteMode: DeletePurge,
		Actions: []auth.Action{{Name: "import", Kind: auth.KindCreate}},
		Hooks: Hooks[*testEntity]{
			ActionCreate: Stage[*testEntity]{Before: func(context.Context, *testEntity) error {
				createCalls++
				return nil
			}},
			"import": Stage[*testEntity]{Before: func(context.Context, *testEntity) error {
				importCalls++
				return nil
			}},
		},
	})
	ctx := context.Background()

	if _, err := app.ExecuteNew(ctx, "import", func(context.Context) (*testEntity, error) {
		return &testEntity{Name: "imported"}, nil
	}); err != nil {
		t.Fatalf("ExecuteNew(import): %v", err)
	}
	if importCalls != 1 || createCalls != 0 {
		t.Fatalf("import=%d create=%d, want import=1 create=0", importCalls, createCalls)
	}
}

func TestUpdateCommandBeforeHookValidatesFinalState(t *testing.T) {
	app, _ := newTestApplication(t, Options[*testEntity, int64]{DeleteMode: DeletePurge,
		Hooks: Hooks[*testEntity]{ActionUpdate: Stage[*testEntity]{Before: func(_ context.Context, entity *testEntity) error {
			if entity.Name == "forbidden" {
				return errors.NewCode(errors.Validation, "forbidden name")
			}
			return nil
		}}},
	})
	ctx := context.Background()
	first := mustCreate(t, app, ctx, "original")
	second := mustCreate(t, app, ctx, "second")
	_, err := app.Execute(ctx, ActionUpdate, first.GetID(), func(_ context.Context, entity *testEntity) error {
		entity.Name = "forbidden"
		return nil
	})
	if !errors.Is(err, errors.Validation) {
		t.Fatalf("Execute error=%v, want Validation", err)
	}
	_, err = app.ExecuteBulk(ctx, ActionUpdate, []int64{first.GetID(), second.GetID()}, func(_ context.Context, entity *testEntity) error {
		entity.Name = "allowed"
		if entity.GetID() == second.GetID() {
			entity.Name = "forbidden"
		}
		return nil
	})
	if !errors.Is(err, errors.Validation) {
		t.Fatalf("ExecuteBulk error=%v, want Validation", err)
	}
	for _, original := range []*testEntity{first, second} {
		stored, err := app.Get(ctx, original.GetID())
		if err != nil {
			t.Fatal(err)
		}
		if stored.Name != original.Name || stored.GetVersion() != 0 {
			t.Fatalf("failed command persisted: %+v", stored)
		}
	}
}
