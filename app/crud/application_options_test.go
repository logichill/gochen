package crud

import (
	"context"
	"testing"

	"gochen/errors"
)

func TestApplicationOptionsAreCapturedAtConstruction(t *testing.T) {
	cfg := DefaultServiceConfig()
	cfg.MaxBatchSize = 1
	validator := &trackingValidator{}
	hookCalls := 0
	hooks := &Hooks[testEntity, int64]{BeforeCreate: func(context.Context, testEntity) error {
		hookCalls++
		return nil
	}}
	opts := &Options[testEntity, int64]{Config: cfg, Validator: validator, Hooks: hooks}
	app, err := NewApplication(newBatchTestRepo(), opts)
	if err != nil {
		t.Fatal(err)
	}

	// 调用方继续使用配置对象，不能改写已构造应用的行为。
	cfg.AutoValidate = false
	cfg.MaxBatchSize = 100
	hooks.BeforeCreate = func(context.Context, testEntity) error { return errors.New("replacement hook") }
	*opts = Options[testEntity, int64]{}
	app.Config().MaxBatchSize = 200
	if err := app.Create(context.Background(), testEntity{id: 1}); err != nil {
		t.Fatal(err)
	}
	if hookCalls != 1 || validator.called != 1 {
		t.Fatalf("original hook/validator calls = %d/%d, want 1/1", hookCalls, validator.called)
	}
	if err := app.CreateAll(context.Background(), []testEntity{{id: 2}, {id: 3}}); !errors.Is(err, errors.Validation) {
		t.Fatalf("original batch limit must still apply, got %v", err)
	}
	if hookCalls != 1 || validator.called != 1 {
		t.Fatal("oversized batch must be rejected before hooks and validation")
	}
}
