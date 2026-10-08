package audited

import (
	"context"

	"testing"
	"time"

	appcrud "gochen/app/crud"
	"gochen/contextx"

	"gochen/domain/audited"
	"gochen/domain/crud"
	"gochen/errors"
)

// TestAuditedApplication_Create_FailsFastWithoutOperator 验证 AuditedApplication Create FailsFastWithoutOperator。
func TestAuditedApplication_Create_FailsFastWithoutOperator(t *testing.T) {
	repo := newMemAuditedRepo()
	store := &recordingAuditStore{}
	app, err := NewApplication(repo, store, nil)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	e := &auditedTestEntity{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1}}}
	if err := app.Create(context.Background(), e); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected InvalidInput, got %v", err)
	}
	if repo.creates != 0 {
		t.Fatalf("expected Create not persisted when operator missing, got creates=%d", repo.creates)
	}
}

// TestAuditedApplication_Create_WritesAuditRecord 验证 AuditedApplication Create WritesAuditRecord。
func TestAuditedApplication_Create_WritesAuditRecord(t *testing.T) {
	repo := newMemAuditedRepo()
	store := &recordingAuditStore{}
	app, err := NewApplication(repo, store, nil)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	ctx, err := contextx.WithOperator(context.Background(), "alice")
	if err != nil {
		t.Fatalf("WithOperator returned error: %v", err)
	}
	e := &auditedTestEntity{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1}}}
	before := time.Now()
	if err := app.Create(ctx, e); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	after := time.Now()
	if repo.creates != 1 {
		t.Fatalf("expected creates=1, got %d", repo.creates)
	}
	if len(store.records) != 1 {
		t.Fatalf("expected 1 audit record, got %d", len(store.records))
	}
	if store.records[0].Operation != audited.AuditOpCreate || store.records[0].Operator != "alice" || store.records[0].EntityID != "1" {
		t.Fatalf("unexpected audit record: %+v", store.records[0])
	}
	if ts := store.records[0].Timestamp; ts.Before(before) || ts.After(after) {
		t.Fatalf("expected audit timestamp within [%v, %v], got %v", before, after, ts)
	}
}

func TestAuditedApplication_Create_PostCommitHookRunsAfterOuterCommit(t *testing.T) {
	repo := newMemAuditedRepo()
	store := &recordingAuditStore{}

	var afterCreateSawCommitted bool
	var postCommitSawCommitted bool
	app, err := NewApplication(repo, store, &appcrud.Options[*auditedTestEntity, int64]{Hooks: &appcrud.Hooks[*auditedTestEntity, int64]{
		AfterCreate: func(ctx context.Context, entity *auditedTestEntity) error {
			_, afterCreateSawCommitted = repo.items[entity.GetID()]
			return nil
		},
		PostCommitCreate: func(ctx context.Context, entity *auditedTestEntity) error {
			_, postCommitSawCommitted = repo.items[entity.GetID()]
			return nil
		},
	}})
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	ctx, err := contextx.WithOperator(context.Background(), "alice")
	if err != nil {
		t.Fatalf("WithOperator returned error: %v", err)
	}
	e := &auditedTestEntity{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1}}}
	if err := app.Create(ctx, e); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if afterCreateSawCommitted {
		t.Fatalf("expected after hook to run before outer transaction commit")
	}
	if !postCommitSawCommitted {
		t.Fatalf("expected post-commit hook to observe outer transaction commit")
	}
}

func TestAuditedApplication_Create_RollsBackWhenAfterCreateFails(t *testing.T) {
	repo := newMemAuditedRepo()
	store := &recordingAuditStore{}

	app, err := NewApplication(repo, store, &appcrud.Options[*auditedTestEntity, int64]{Hooks: &appcrud.Hooks[*auditedTestEntity, int64]{
		AfterCreate: func(ctx context.Context, entity *auditedTestEntity) error {
			return errors.NewCode(errors.Internal, "after create failed")
		},
	}})
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	ctx, err := contextx.WithOperator(context.Background(), "alice")
	if err != nil {
		t.Fatalf("WithOperator returned error: %v", err)
	}
	e := &auditedTestEntity{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1}}}
	if err := app.Create(ctx, e); err == nil {
		t.Fatalf("expected create to fail when after hook fails")
	}
	if _, err := repo.Get(context.Background(), 1); !errors.Is(err, errors.NotFound) {
		t.Fatalf("expected entity not persisted after rollback, got err=%v", err)
	}
	if len(store.records) != 0 {
		t.Fatalf("expected no audit record after rollback, got %d", len(store.records))
	}
}

func TestAuditedApplication_Create_RollsBackWhenSnapshotMarshalFails(t *testing.T) {
	repo := newMemAuditedRepo()
	store := &recordingAuditStore{}
	app, err := NewApplication(repo, store, nil)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	ctx, err := contextx.WithOperator(context.Background(), "alice")
	if err != nil {
		t.Fatalf("WithOperator returned error: %v", err)
	}
	e := &auditedTestEntity{
		AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1}},
		Snapshot:      make(chan int),
	}
	if err := app.Create(ctx, e); !errors.Is(err, errors.Internal) {
		t.Fatalf("expected Internal when snapshot marshal fails, got %v", err)
	}
	if _, err := repo.Get(context.Background(), 1); !errors.Is(err, errors.NotFound) {
		t.Fatalf("expected entity not persisted when snapshot marshal fails, got err=%v", err)
	}
	if len(store.records) != 0 {
		t.Fatalf("expected no audit record after snapshot marshal failure, got %d", len(store.records))
	}
}

func TestAuditedApplication_Update_PreservesManagedFields(t *testing.T) {
	repo := newMemAuditedRepo()
	store := &recordingAuditStore{}
	app, err := NewApplication(repo, store, nil)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}
	createdAt := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	updatedAt := time.Date(2024, 2, 3, 4, 5, 6, 0, time.UTC)
	original := &auditedTestEntity{
		AuditedEntity: &audited.AuditedEntity[int64]{
			Entity:    crud.Entity[int64]{ID: 1, Version: 1},
			CreatedAt: createdAt,
			CreatedBy: "owner",
			UpdatedAt: updatedAt,
			UpdatedBy: "previous-editor",
		},
		Name: "original",
	}
	repo.items[1] = original

	ctx, err := contextx.WithOperator(context.Background(), "alice")
	if err != nil {
		t.Fatalf("WithOperator returned error: %v", err)
	}
	updated := &auditedTestEntity{
		AuditedEntity: &audited.AuditedEntity[int64]{
			Entity:    crud.Entity[int64]{ID: 1, Version: 1},
			CreatedAt: time.Now(),
			CreatedBy: "attacker",
			UpdatedAt: time.Now(),
			UpdatedBy: "attacker",
			DeletedBy: func() *string { v := "attacker"; return &v }(),
		},
		Name: "updated",
	}
	if err := app.Update(ctx, updated); err != nil {
		t.Fatalf("update: %v", err)
	}
	got := repo.items[1]
	if !got.CreatedAt.Equal(createdAt) || got.CreatedBy != "owner" {
		t.Fatalf("managed created fields changed: at=%v by=%q", got.CreatedAt, got.CreatedBy)
	}
	if got.IsDeleted() {
		t.Fatalf("regular update must not soft-delete an entity")
	}
	if got.DeletedBy != nil {
		t.Fatalf("regular update replaced managed deleted_by: %v", *got.DeletedBy)
	}
	if !got.UpdatedAt.Equal(updatedAt) || got.UpdatedBy != "previous-editor" {
		t.Fatalf("managed updated fields changed: at=%v by=%q", got.UpdatedAt, got.UpdatedBy)
	}
}
