package crud

import (
	"context"
	"testing"

	"gochen/errors"
)

// typed nil 指针实体必须 fail-fast 返回 InvalidInput，而不是 panic 或落库。
func TestApplicationWriteRejectsTypedNilEntity(t *testing.T) {
	app, _ := newTestApplication(t, Options[*testEntity, int64]{})
	ctx := context.Background()
	var nilEntity *testEntity

	if err := app.Create(ctx, nilEntity); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("Create(typed nil) = %v, want InvalidInput", err)
	}
	if err := app.Update(ctx, nilEntity); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("Update(typed nil) = %v, want InvalidInput", err)
	}
	if _, err := app.ExecuteNew(ctx, ActionCreate, func(context.Context) (*testEntity, error) {
		return nilEntity, nil
	}); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("ExecuteNew(typed nil) = %v, want InvalidInput", err)
	}
}

func TestApplicationBulkWriteRejectsTypedNilEntity(t *testing.T) {
	app, _ := newTestApplication(t, Options[*testEntity, int64]{})
	ctx := context.Background()
	var nilEntity *testEntity

	if _, err := app.ExecuteNewBulk(ctx, ActionCreate, func(context.Context) ([]*testEntity, error) {
		return []*testEntity{{Name: "ok"}, nilEntity}, nil
	}); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("ExecuteNewBulk(typed nil) = %v, want InvalidInput", err)
	}
	count, err := app.Count(ctx)
	if err != nil || count != 0 {
		t.Fatalf("rejected bulk must roll back, Count() = %d, %v", count, err)
	}
}

func TestApplicationRejectsNilContext(t *testing.T) {
	app, _ := newTestApplication(t, Options[*testEntity, int64]{})
	var ctx context.Context

	if err := app.Create(ctx, &testEntity{Name: "x"}); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("Create(nil ctx) = %v, want InvalidInput", err)
	}
	if _, err := app.Get(ctx, 1); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("Get(nil ctx) = %v, want InvalidInput", err)
	}
	if _, err := app.List(ctx, 0, 10); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("List(nil ctx) = %v, want InvalidInput", err)
	}
	if err := app.Delete(ctx, 1); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("Delete(nil ctx) = %v, want InvalidInput", err)
	}
	if _, err := app.ExecuteNewBulk(ctx, ActionCreate, func(context.Context) ([]*testEntity, error) {
		return nil, nil
	}); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("ExecuteNewBulk(nil ctx) = %v, want InvalidInput", err)
	}
}
