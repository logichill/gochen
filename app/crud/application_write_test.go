package crud

import (
	"context"
	"testing"

	"gochen/app"
	"gochen/auth"
	"gochen/errors"
)

func TestCreateAssignsIDAndPersists(t *testing.T) {
	app, _ := newTestApplication(t, Options[*testEntity, int64]{DeleteMode: DeletePurge})
	ctx := context.Background()
	entity := &testEntity{Name: "first", Active: true}
	if err := app.Create(ctx, entity); err != nil {
		t.Fatalf("Create(): %v", err)
	}
	if entity.GetID() != 1 {
		t.Fatalf("generated ID = %d", entity.GetID())
	}
	stored, err := app.Get(ctx, entity.GetID())
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}
	if stored.Name != "first" || !stored.Active || stored.GetVersion() != 0 {
		t.Fatalf("stored = %+v", stored)
	}

	if err := app.Create(ctx, &testEntity{Entity: domaincrudEntity(1), Name: "duplicate"}); !errors.Is(err, errors.Conflict) {
		t.Fatalf("Create(duplicate ID) error = %v, want Conflict", err)
	}
}

func TestUpdatePersistsChangesAndAdvancesVersion(t *testing.T) {
	app, _ := newTestApplication(t, Options[*testEntity, int64]{DeleteMode: DeletePurge})
	ctx := context.Background()
	created := mustCreate(t, app, ctx, "before")

	loaded, err := app.Get(ctx, created.GetID())
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}
	loaded.Name = "after"
	loaded.Active = true
	if err := app.Update(ctx, loaded); err != nil {
		t.Fatalf("Update(): %v", err)
	}
	stored, err := app.Get(ctx, created.GetID())
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}
	if stored.Name != "after" || !stored.Active {
		t.Fatalf("stored = %+v", stored)
	}
	if stored.GetVersion() != 1 {
		t.Fatalf("version = %d, want 1", stored.GetVersion())
	}
}

func TestUpdateWithoutChangesSkipsWrite(t *testing.T) {
	app, _ := newTestApplication(t, Options[*testEntity, int64]{DeleteMode: DeletePurge})
	ctx := context.Background()
	created := mustCreate(t, app, ctx, "same")
	loaded, err := app.Get(ctx, created.GetID())
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}
	if err := app.Update(ctx, loaded); err != nil {
		t.Fatalf("Update(): %v", err)
	}
	stored, err := app.Get(ctx, created.GetID())
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}
	if stored.GetVersion() != 0 {
		t.Fatalf("no-op update advanced version to %d", stored.GetVersion())
	}
}

func TestUpdateRejectsStaleVersion(t *testing.T) {
	app, _ := newTestApplication(t, Options[*testEntity, int64]{DeleteMode: DeletePurge})
	ctx := context.Background()
	created := mustCreate(t, app, ctx, "original")

	first, err := app.Get(ctx, created.GetID())
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}
	second, err := app.Get(ctx, created.GetID())
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}

	first.Name = "first"
	if err := app.Update(ctx, first); err != nil {
		t.Fatalf("Update(first): %v", err)
	}

	second.Name = "second"
	if err := app.Update(ctx, second); !errors.Is(err, errors.Concurrency) {
		t.Fatalf("Update(stale) error = %v, want Concurrency", err)
	}
	stored, err := app.Get(ctx, created.GetID())
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}
	if stored.Name != "first" || stored.GetVersion() != 1 {
		t.Fatalf("stored = %+v", stored)
	}
}

func TestUpdateMissingEntityReturnsNotFound(t *testing.T) {
	app, _ := newTestApplication(t, Options[*testEntity, int64]{DeleteMode: DeletePurge})
	if err := app.Update(context.Background(), &testEntity{Entity: domaincrudEntity(42), Name: "x"}); !errors.Is(err, errors.NotFound) {
		t.Fatalf("Update(missing) error = %v, want NotFound", err)
	}
}

func TestDeleteRemovesEntity(t *testing.T) {
	app, _ := newTestApplication(t, Options[*testEntity, int64]{DeleteMode: DeletePurge})
	ctx := context.Background()
	created := mustCreate(t, app, ctx, "doomed")
	if err := app.Delete(ctx, created.GetID()); err != nil {
		t.Fatalf("Delete(): %v", err)
	}
	if _, err := app.Get(ctx, created.GetID()); !errors.Is(err, errors.NotFound) {
		t.Fatalf("Get(deleted) error = %v, want NotFound", err)
	}
	count, err := app.Count(ctx)
	if err != nil || count != 0 {
		t.Fatalf("Count() = %d, %v", count, err)
	}
	if err := app.Delete(ctx, created.GetID()); !errors.Is(err, errors.NotFound) {
		t.Fatalf("Delete(missing) error = %v, want NotFound", err)
	}
}

func TestDeleteBulkIsAtomicAndAuthorizesOnce(t *testing.T) {
	authorizer := newCountingAuthorizer()
	app, _ := newTestApplication(t, Options[*testEntity, int64]{DeleteMode: DeletePurge, Authorizer: authorizer})
	ctx := context.Background()
	first := mustCreate(t, app, ctx, "a")
	second := mustCreate(t, app, ctx, "b")
	mustCreate(t, app, ctx, "c")

	if err := app.DeleteBulk(ctx, []int64{first.GetID(), second.GetID()}); err != nil {
		t.Fatalf("DeleteBulk(): %v", err)
	}
	if authorizer.calls[ActionPurge] != 1 {
		t.Fatalf("authorize calls = %d, want 1", authorizer.calls[ActionPurge])
	}
	count, err := app.Count(ctx)
	if err != nil || count != 1 {
		t.Fatalf("Count() = %d, %v", count, err)
	}
}

func TestDeleteBulkValidation(t *testing.T) {
	app, _ := newTestApplication(t, Options[*testEntity, int64]{DeleteMode: DeletePurge,
		Limits: app.Limits{MaxBulkSize: 2},
	})
	ctx := context.Background()
	mustCreate(t, app, ctx, "a")
	mustCreate(t, app, ctx, "b")

	if err := app.DeleteBulk(ctx, nil); err != nil {
		t.Fatalf("DeleteBulk(empty) = %v", err)
	}
	if err := app.DeleteBulk(ctx, []int64{1, 1}); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("DeleteBulk(duplicate) = %v, want InvalidInput", err)
	}
	if err := app.DeleteBulk(ctx, []int64{1, 2, 3}); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("DeleteBulk(over limit) = %v, want InvalidInput", err)
	}
	if err := app.DeleteBulk(ctx, []int64{1, 99}); !errors.Is(err, errors.NotFound) {
		t.Fatalf("DeleteBulk(missing) = %v, want NotFound", err)
	}
	count, err := app.Count(ctx)
	if err != nil || count != 2 {
		t.Fatalf("rollback failed, Count() = %d, %v", count, err)
	}
}

func TestExecuteChangesLoadedEntity(t *testing.T) {
	app, _ := newTestApplication(t, Options[*testEntity, int64]{DeleteMode: DeletePurge})
	ctx := context.Background()
	created := mustCreate(t, app, ctx, "before")

	updated, err := app.Execute(ctx, ActionUpdate, created.GetID(), func(_ context.Context, entity *testEntity) error {
		entity.Name = "after"
		return nil
	})
	if err != nil {
		t.Fatalf("Execute(): %v", err)
	}
	if updated.Name != "after" || updated.GetVersion() != 1 {
		t.Fatalf("Execute() = %+v", updated)
	}
	stored, err := app.Get(ctx, created.GetID())
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}
	if stored.Name != "after" {
		t.Fatalf("stored name = %q", stored.Name)
	}
}

func TestExecuteNoOpDoesNotWrite(t *testing.T) {
	app, _ := newTestApplication(t, Options[*testEntity, int64]{DeleteMode: DeletePurge})
	ctx := context.Background()
	created := mustCreate(t, app, ctx, "same")
	result, err := app.Execute(ctx, ActionUpdate, created.GetID(), func(context.Context, *testEntity) error {
		return nil
	})
	if err != nil {
		t.Fatalf("Execute(): %v", err)
	}
	if result.GetVersion() != 0 {
		t.Fatalf("version = %d", result.GetVersion())
	}
}

func TestExecuteErrors(t *testing.T) {
	app, _ := newTestApplication(t, Options[*testEntity, int64]{DeleteMode: DeletePurge,
		Actions: []auth.Action{{Name: "import", Kind: auth.KindCreate}},
	})
	ctx := context.Background()
	created := mustCreate(t, app, ctx, "a")

	want := errors.New("command failed")
	_, err := app.Execute(ctx, ActionUpdate, created.GetID(), func(_ context.Context, entity *testEntity) error {
		entity.Name = "should not persist"
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("Execute(error) = %v", err)
	}
	stored, err := app.Get(ctx, created.GetID())
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}
	if stored.Name != "a" || stored.GetVersion() != 0 {
		t.Fatalf("failed command persisted changes: %+v", stored)
	}

	if _, err := app.Execute(ctx, ActionUpdate, 99, func(context.Context, *testEntity) error { return nil }); !errors.Is(err, errors.NotFound) {
		t.Fatalf("Execute(missing) = %v, want NotFound", err)
	}
	if _, err := app.Execute(ctx, ActionUpdate, created.GetID(), nil); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("Execute(nil fn) = %v, want InvalidInput", err)
	}
	if _, err := app.Execute(ctx, "import", created.GetID(), func(context.Context, *testEntity) error { return nil }); !errors.Is(err, errors.Internal) {
		t.Fatalf("Execute(kind mismatch) = %v, want Internal", err)
	}
}

func TestExecuteNewCreatesEntity(t *testing.T) {
	app, _ := newTestApplication(t, Options[*testEntity, int64]{DeleteMode: DeletePurge})
	ctx := context.Background()
	created, err := app.ExecuteNew(ctx, ActionCreate, func(context.Context) (*testEntity, error) {
		return &testEntity{Name: "new"}, nil
	})
	if err != nil {
		t.Fatalf("ExecuteNew(): %v", err)
	}
	if created.GetID() == 0 {
		t.Fatal("expected generated ID")
	}
	stored, err := app.Get(ctx, created.GetID())
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}
	if stored.Name != "new" {
		t.Fatalf("stored = %+v", stored)
	}

	if _, err := app.ExecuteNew(ctx, ActionCreate, func(context.Context) (*testEntity, error) {
		return nil, nil
	}); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("ExecuteNew(nil entity) = %v, want InvalidInput", err)
	}
}

func TestExecuteNewBulkCreatesAndValidatesLimit(t *testing.T) {
	app, _ := newTestApplication(t, Options[*testEntity, int64]{DeleteMode: DeletePurge,
		Limits: app.Limits{MaxBulkSize: 2},
	})
	ctx := context.Background()

	created, err := app.ExecuteNewBulk(ctx, ActionCreate, func(context.Context) ([]*testEntity, error) {
		return []*testEntity{{Name: "a"}, {Name: "b"}}, nil
	})
	if err != nil {
		t.Fatalf("ExecuteNewBulk(): %v", err)
	}
	if len(created) != 2 || created[0].GetID() == 0 || created[1].GetID() == 0 {
		t.Fatalf("created = %+v", created)
	}
	count, err := app.Count(ctx)
	if err != nil || count != 2 {
		t.Fatalf("Count() = %d, %v", count, err)
	}

	if _, err := app.ExecuteNewBulk(ctx, ActionCreate, func(context.Context) ([]*testEntity, error) {
		return []*testEntity{{Name: "c"}, {Name: "d"}, {Name: "e"}}, nil
	}); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("ExecuteNewBulk(over limit) = %v, want InvalidInput", err)
	}
	count, err = app.Count(ctx)
	if err != nil || count != 2 {
		t.Fatalf("over-limit bulk must roll back, Count() = %d, %v", count, err)
	}

	empty, err := app.ExecuteNewBulk(ctx, ActionCreate, func(context.Context) ([]*testEntity, error) {
		return nil, nil
	})
	if err != nil {
		t.Fatalf("ExecuteNewBulk(empty) = %v", err)
	}
	if empty == nil || len(empty) != 0 {
		t.Fatalf("empty result = %+v", empty)
	}

	if _, err := app.ExecuteNewBulk(ctx, ActionCreate, nil); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("ExecuteNewBulk(nil fn) = %v, want InvalidInput", err)
	}
}

func TestExecuteBulkUpdatesInOneTransaction(t *testing.T) {
	app, _ := newTestApplication(t, Options[*testEntity, int64]{DeleteMode: DeletePurge})
	ctx := context.Background()
	first := mustCreate(t, app, ctx, "a")
	second := mustCreate(t, app, ctx, "b")

	updated, err := app.ExecuteBulk(ctx, ActionUpdate, []int64{first.GetID(), second.GetID()}, func(_ context.Context, entity *testEntity) error {
		entity.Active = true
		return nil
	})
	if err != nil {
		t.Fatalf("ExecuteBulk(): %v", err)
	}
	if len(updated) != 2 || !updated[0].Active || !updated[1].Active {
		t.Fatalf("updated = %+v", updated)
	}

	if _, err := app.ExecuteBulk(ctx, ActionUpdate, []int64{first.GetID(), first.GetID()}, func(context.Context, *testEntity) error {
		return nil
	}); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("ExecuteBulk(duplicate) = %v, want InvalidInput", err)
	}

	if _, err := app.ExecuteBulk(ctx, ActionUpdate, []int64{first.GetID(), 99}, func(_ context.Context, entity *testEntity) error {
		entity.Name = "rollback"
		return nil
	}); !errors.Is(err, errors.NotFound) {
		t.Fatalf("ExecuteBulk(missing) = %v, want NotFound", err)
	}
	stored, err := app.Get(ctx, first.GetID())
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}
	if stored.Name != "a" {
		t.Fatalf("missing-id bulk must roll back, stored = %+v", stored)
	}

	want := errors.New("second failed")
	if _, err := app.ExecuteBulk(ctx, ActionUpdate, []int64{first.GetID(), second.GetID()}, func(_ context.Context, entity *testEntity) error {
		if entity.GetID() == second.GetID() {
			return want
		}
		entity.Name = "changed"
		return nil
	}); !errors.Is(err, want) {
		t.Fatalf("ExecuteBulk(fn error) = %v", err)
	}
	stored, err = app.Get(ctx, first.GetID())
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}
	if stored.Name != "a" {
		t.Fatalf("fn error must roll back the bulk, stored = %+v", stored)
	}

	empty, err := app.ExecuteBulk(ctx, ActionUpdate, nil, func(context.Context, *testEntity) error { return nil })
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("ExecuteBulk(empty) = %+v, %v", empty, err)
	}
}
