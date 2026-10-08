package crud

import (
	"context"
	"strconv"

	"testing"

	"gochen/app/operation"

	"gochen/errors"
)

func TestApplication_CreateWrappedByOperationRunner(t *testing.T) {
	repo := &recordingRepo{}
	app, err := NewApplication(repo, nil)
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	spec := &operation.Spec{
		Type:           "entity.create",
		Mode:           operation.ModeInline,
		Resource:       &operation.Resource{Type: "entity"},
		AffectedScopes: []string{"entities:list"},
	}
	entity := testEntity{id: 12}
	result, err := operation.DefaultRunner().Execute(context.Background(), spec, func(ctx context.Context) (*operation.Result, error) {
		if err := app.Create(ctx, entity); err != nil {
			return nil, err
		}
		return operation.MergeResult(&operation.Result{
			Result: map[string]any{"entity_id": int64(12)},
		}, spec, &operation.Resource{ID: strconv.FormatInt(entity.GetID(), 10)}), nil
	})
	if err != nil {
		t.Fatalf("create wrapped by operation runner: %v", err)
	}
	if result.Operation.Type != "entity.create" {
		t.Fatalf("unexpected type: %s", result.Operation.Type)
	}
	if result.Operation.Status != operation.StatusSettled {
		t.Fatalf("unexpected status: %s", result.Operation.Status)
	}
	if result.Resource == nil || result.Resource.Type != "entity" || result.Resource.ID != "12" {
		t.Fatalf("unexpected resource: %+v", result.Resource)
	}
	if len(result.AffectedScopes) != 1 || result.AffectedScopes[0] != "entities:list" {
		t.Fatalf("unexpected affected scopes: %+v", result.AffectedScopes)
	}
}

// TestApplication_Create_ValidatesBeforePersist 验证 Application Create ValidatesBeforePersist。
func TestApplication_Create_ValidatesBeforePersist(t *testing.T) {
	repo := &recordingRepo{}
	v := &trackingValidator{err: errors.New("validator hit")}
	cfg := DefaultServiceConfig()
	cfg.AutoValidate = true

	app, err := NewApplication(repo, &Options[testEntity, int64]{Validator: v, Config: cfg})
	if err != nil {
		t.Fatalf("new application: %v", err)
	}
	if err := app.Create(context.Background(), testEntity{id: 1}); !errors.Is(err, v.err) {
		t.Fatalf("expected validator error, got %v", err)
	}
	if v.called != 1 {
		t.Fatalf("expected validator to be called once, got %d", v.called)
	}
}

// TestApplication_Update_ValidatesBeforePersist 验证 Application Update ValidatesBeforePersist。
func TestApplication_Update_ValidatesBeforePersist(t *testing.T) {
	repo := &recordingRepo{}
	v := &trackingValidator{err: errors.New("validator hit")}
	cfg := DefaultServiceConfig()
	cfg.AutoValidate = true

	app, err := NewApplication(repo, &Options[testEntity, int64]{Validator: v, Config: cfg})
	if err != nil {
		t.Fatalf("new application: %v", err)
	}
	if err := app.Update(context.Background(), testEntity{id: 1}); !errors.Is(err, v.err) {
		t.Fatalf("expected validator error, got %v", err)
	}
	if v.called != 1 {
		t.Fatalf("expected validator to be called once, got %d", v.called)
	}
}
