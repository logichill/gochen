package crud

import (
	"context"
	"testing"

	"gochen/app/internal/execution"
	"gochen/auth"
	"gochen/errors"
)

func TestOptionsAreCapturedAtConstruction(t *testing.T) {
	hookCalls := 0
	hooks := Hooks[*testEntity]{
		ActionCreate: Stage[*testEntity]{Before: func(context.Context, *testEntity) error {
			hookCalls++
			return nil
		}},
	}
	actions := []auth.Action{{Name: "rename", Kind: auth.KindUpdate}}
	app, _ := newTestApplication(t, Options[*testEntity, int64]{
		Actions: actions,
		Hooks:   hooks,
		Limits:  execution.Limits{MaxBulkSize: 1, MaxPageSize: 1},
	})

	// 调用方继续修改共享的 map/slice，不得影响已构造应用。
	delete(hooks, ActionCreate)
	actions[0].Name = "changed"

	entity := mustCreate(t, app, context.Background(), "one")
	if hookCalls != 1 {
		t.Fatalf("hook calls = %d, want 1", hookCalls)
	}
	if _, err := app.Execute(context.Background(), "rename", entity.GetID(), func(_ context.Context, e *testEntity) error {
		e.Name = "renamed"
		return nil
	}); err != nil {
		t.Fatalf("Execute(rename): %v", err)
	}
	if _, err := app.Execute(context.Background(), "changed", entity.GetID(), func(context.Context, *testEntity) error {
		return nil
	}); !errors.Is(err, errors.Forbidden) {
		t.Fatalf("mutated action must not be declared, got %v", err)
	}

	if app.MaxBulkSize() != 1 || app.MaxPageSize() != 1 {
		t.Fatalf("limits = %d/%d", app.MaxBulkSize(), app.MaxPageSize())
	}
	if err := app.DeleteBulk(context.Background(), []int64{1, 2}); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("bulk over limit = %v, want InvalidInput", err)
	}
	items, err := app.List(context.Background(), 0, 10)
	if err != nil {
		t.Fatalf("List(): %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("page size limit not applied, got %d items", len(items))
	}
}

func TestLimitsNormalizeNonPositiveValues(t *testing.T) {
	app, _ := newTestApplication(t, Options[*testEntity, int64]{
		Limits: execution.Limits{MaxBulkSize: -1, MaxPageSize: 0},
	})
	if app.MaxBulkSize() != 1000 || app.MaxPageSize() != 1000 {
		t.Fatalf("normalized limits = %d/%d", app.MaxBulkSize(), app.MaxPageSize())
	}
}

func TestDescriptorReturnsCopy(t *testing.T) {
	app, _ := newTestApplication(t, Options[*testEntity, int64]{})
	first := app.Descriptor()
	first.Actions[0].Name = "mutated"
	second := app.Descriptor()
	if second.Actions[0].Name == "mutated" {
		t.Fatal("Descriptor must return a copy of its action list")
	}
}
