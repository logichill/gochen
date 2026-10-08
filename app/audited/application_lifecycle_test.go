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

// TestAuditedApplication_Delete_SoftDeletesAndWritesAudit 验证 AuditedApplication Delete SoftDeletesAndWritesAudit。
func TestAuditedApplication_Delete_SoftDeletesAndWritesAudit(t *testing.T) {
	repo := newMemAuditedRepo()
	store := &recordingAuditStore{}
	app, err := NewApplication(repo, store, nil)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	// 预置一条数据
	e := &auditedTestEntity{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1}}}
	repo.items[1] = e

	ctx, err := contextx.WithOperator(context.Background(), "bob")
	if err != nil {
		t.Fatalf("WithOperator returned error: %v", err)
	}
	if err := app.Delete(ctx, 1); err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	got, err := repo.Get(context.Background(), 1)
	if err != nil {
		t.Fatalf("get after delete: %v", err)
	}
	if !got.IsDeleted() {
		t.Fatalf("expected entity to be soft-deleted")
	}
	if repo.updates != 1 {
		t.Fatalf("expected updates=1, got %d", repo.updates)
	}
	if len(store.records) != 1 {
		t.Fatalf("expected 1 audit record, got %d", len(store.records))
	}
	if store.records[0].Operation != audited.AuditOpDelete || store.records[0].Operator != "bob" {
		t.Fatalf("unexpected audit record: %+v", store.records[0])
	}
}

// TestAuditedApplication_Purge_FailsFastWithoutOperator 验证 AuditedApplication Purge FailsFastWithoutOperator。
func TestAuditedApplication_Purge_FailsFastWithoutOperator(t *testing.T) {
	repo := newMemAuditedRepo()
	store := &recordingAuditStore{}
	app, err := NewApplication(repo, store, nil)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	repo.items[1] = &auditedTestEntity{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1}}}
	err = app.Purge(context.Background(), 1)
	if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected InvalidInput, got %v", err)
	}
	if repo.purges != 0 {
		t.Fatalf("expected purge not executed when operator missing, got purges=%d", repo.purges)
	}
}

// TestAuditedApplication_Purge_DeletesAndWritesAudit 验证 AuditedApplication Purge DeletesAndWritesAudit。
func TestAuditedApplication_Purge_DeletesAndWritesAudit(t *testing.T) {
	repo := newMemAuditedRepo()
	store := &recordingAuditStore{}
	app, err := NewApplication(repo, store, nil)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	repo.items[1] = &auditedTestEntity{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1}}}
	ctx, err := contextx.WithOperator(context.Background(), "carol")
	if err != nil {
		t.Fatalf("WithOperator returned error: %v", err)
	}
	if err := app.Purge(ctx, 1); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if repo.purges != 1 {
		t.Fatalf("expected purges=1, got %d", repo.purges)
	}
	if _, err := repo.Get(context.Background(), 1); !errors.Is(err, errors.NotFound) {
		t.Fatalf("expected entity to be removed (NotFound), got err=%v", err)
	}
	if len(store.records) != 1 || store.records[0].Operation != audited.AuditOpDeleteHard || store.records[0].Operator != "carol" {
		t.Fatalf("unexpected audit record: %+v", store.records)
	}
}

func TestAuditedApplication_Purge_PostCommitHookRunsAfterOuterCommit(t *testing.T) {
	repo := newMemAuditedRepo()
	store := &recordingAuditStore{}

	var afterDeleteBeforeCommit bool
	var postCommitSawRemoved bool
	app, err := NewApplication(repo, store, &appcrud.Options[*auditedTestEntity, int64]{Hooks: &appcrud.Hooks[*auditedTestEntity, int64]{
		AfterDelete: func(ctx context.Context, id int64) error {
			_, present := repo.items[id]
			afterDeleteBeforeCommit = present
			return nil
		},
		PostCommitDelete: func(ctx context.Context, id int64) error {
			_, present := repo.items[id]
			postCommitSawRemoved = !present
			return nil
		},
	}})
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	repo.items[1] = &auditedTestEntity{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1}}}
	ctx, err := contextx.WithOperator(context.Background(), "carol")
	if err != nil {
		t.Fatalf("WithOperator returned error: %v", err)
	}
	if err := app.Purge(ctx, 1); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if !afterDeleteBeforeCommit {
		t.Fatalf("expected after-delete hook to run before outer transaction commit")
	}
	if !postCommitSawRemoved {
		t.Fatalf("expected post-commit delete hook to run after outer commit")
	}
}

func TestAuditedApplication_GetWithDeleted_DelegatesToRestoreRepository(t *testing.T) {
	repo := newMemAuditedRepo()
	store := &recordingAuditStore{}
	app, err := NewApplication(repo, store, nil)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}
	entity := &auditedTestEntity{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1, Version: 7}}}
	_ = entity.SoftDeleteBy("alice", time.Now())
	repo.items[1] = entity

	got, err := app.GetWithDeleted(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetWithDeleted: %v", err)
	}
	if got != entity {
		t.Fatalf("expected stored deleted entity, got %+v", got)
	}
	if !got.IsDeleted() {
		t.Fatalf("expected deleted entity to be returned")
	}
}

// TestAuditedApplication_Delete_AlreadyDeletedReturnsConflict 验证 AuditedApplication Delete AlreadyDeletedReturnsConflict。
func TestAuditedApplication_Delete_AlreadyDeletedReturnsConflict(t *testing.T) {
	repo := newMemAuditedRepo()
	store := &recordingAuditStore{}
	app, err := NewApplication(repo, store, nil)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	// 预置一条已软删的数据
	e := &auditedTestEntity{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1}}}
	_ = e.SoftDeleteBy("alice", time.Now())
	repo.items[1] = e

	ctx, err := contextx.WithOperator(context.Background(), "bob")
	if err != nil {
		t.Fatalf("WithOperator returned error: %v", err)
	}
	err = app.Delete(ctx, 1)
	if !errors.Is(err, errors.Conflict) {
		t.Fatalf("expected Conflict error for already deleted entity, got %v", err)
	}
	// 审计记录不应写入
	if len(store.records) != 0 {
		t.Fatalf("expected no audit record when delete fails, got %d", len(store.records))
	}
}

// TestAuditedApplication_Restore_NotDeletedReturnsConflict 验证 AuditedApplication Restore NotDeletedReturnsConflict。
func TestAuditedApplication_Restore_NotDeletedReturnsConflict(t *testing.T) {
	repo := newMemAuditedRepo()
	store := &recordingAuditStore{}
	app, err := NewApplication(repo, store, nil)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	// 预置一条未删除的数据
	e := &auditedTestEntity{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1}}}
	repo.items[1] = e

	err = app.Restore(context.Background(), 1, "bob")
	if !errors.Is(err, errors.Conflict) {
		t.Fatalf("expected Conflict error for non-deleted entity restore, got %v", err)
	}
	// 审计记录不应写入
	if len(store.records) != 0 {
		t.Fatalf("expected no audit record when restore fails, got %d", len(store.records))
	}
}

func TestAuditedApplication_Restore_PostCommitHookRunsAfterOuterCommit(t *testing.T) {
	repo := newMemAuditedRepo()
	store := &recordingAuditStore{}

	var postCommitSawRestored bool
	app, err := NewApplication(repo, store, &appcrud.Options[*auditedTestEntity, int64]{Hooks: &appcrud.Hooks[*auditedTestEntity, int64]{
		PostCommitUpdate: func(ctx context.Context, entity *auditedTestEntity) error {
			got, ok := repo.items[entity.GetID()]
			postCommitSawRestored = ok && !got.IsDeleted()
			return nil
		},
	}})
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	entity := &auditedTestEntity{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1}}}
	_ = entity.SoftDeleteBy("alice", time.Now())
	repo.items[1] = entity

	if err := app.Restore(context.Background(), 1, "bob"); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if !postCommitSawRestored {
		t.Fatalf("expected post-commit update hook to observe restored entity after outer commit")
	}
}
