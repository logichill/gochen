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
		Delete: appcrud.Stage[int64]{
			After: func(_ context.Context, id int64) error {
				_, present := repo.items[id]
				afterDeleteBeforeCommit = present
				return nil
			},
			PostCommit: func(_ context.Context, id int64) error {
				_, present := repo.items[id]
				postCommitSawRemoved = !present
				return nil
			},
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
		Update: appcrud.Stage[*auditedTestEntity]{PostCommit: func(_ context.Context, entity *auditedTestEntity) error {
			got, ok := repo.items[entity.GetID()]
			postCommitSawRestored = ok && !got.IsDeleted()
			return nil
		}},
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

func TestAuditedApplication_Restore_AfterHookRunsInTx(t *testing.T) {
	repo := newMemAuditedRepo()
	store := &recordingAuditStore{}

	var afterHookTriggered bool
	app, err := NewApplication(repo, store, &appcrud.Options[*auditedTestEntity, int64]{Hooks: &appcrud.Hooks[*auditedTestEntity, int64]{
		Update: appcrud.Stage[*auditedTestEntity]{After: func(_ context.Context, entity *auditedTestEntity) error {
			afterHookTriggered = true
			if entity == nil || entity.IsDeleted() {
				t.Fatalf("expected entity to be restored in After hook, got %v", entity)
			}
			return nil
		}},
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
	if !afterHookTriggered {
		t.Fatalf("expected Update.After hook to be triggered on Restore")
	}
}

func TestAuditedApplication_Restore_TriggersBeforeHookAndValidate(t *testing.T) {
	repo := newMemAuditedRepo()
	store := &recordingAuditStore{}

	var beforeHookTriggered bool
	app, err := NewApplication(repo, store, &appcrud.Options[*auditedTestEntity, int64]{
		Hooks: &appcrud.Hooks[*auditedTestEntity, int64]{
			Update: appcrud.Stage[*auditedTestEntity]{
				Before: func(_ context.Context, entity *auditedTestEntity) error {
					beforeHookTriggered = true
					if entity.ID == 99 {
						return errors.NewCode(errors.Forbidden, "entity 99 cannot be restored")
					}
					return nil
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	// 1. 测试 Before hook 拦截恢复
	entityBlocked := &auditedTestEntity{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 99}}}
	_ = entityBlocked.SoftDeleteBy("alice", time.Now())
	repo.items[99] = entityBlocked

	err = app.Restore(context.Background(), 99, "bob")
	if !errors.Is(err, errors.Forbidden) {
		t.Fatalf("expected forbidden error, got: %v", err)
	}
	if !beforeHookTriggered {
		t.Fatalf("expected Update.Before hook to be triggered on Restore")
	}

	// 2. 测试正常放行
	entityAllowed := &auditedTestEntity{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1}}}
	_ = entityAllowed.SoftDeleteBy("alice", time.Now())
	repo.items[1] = entityAllowed

	beforeHookTriggered = false
	if err := app.Restore(context.Background(), 1, "bob"); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if !beforeHookTriggered {
		t.Fatalf("expected Update.Before hook to be triggered on Restore")
	}
}

// TestAuditedApplication_Purge_UsesDedicatedPurgeHooks 验证配置了 Purge 独立钩子时，
// 物理删除走 Purge Stage 而不是重放 Delete 钩子（P2-085）。
func TestAuditedApplication_Purge_UsesDedicatedPurgeHooks(t *testing.T) {
	repo := newMemAuditedRepo()
	store := &recordingAuditStore{}

	deleteAfterCalls := 0
	purgeAfterCalls := 0
	purgeBeforeCalls := 0
	app, err := NewApplication(repo, store, &appcrud.Options[*auditedTestEntity, int64]{Hooks: &appcrud.Hooks[*auditedTestEntity, int64]{
		Delete: appcrud.Stage[int64]{
			// 模拟 Delete.Before 校验活跃态：已软删记录对常规读不可见，
			// 若 Purge 错误复用 Delete 钩子将在此被卡死（P2-085 报告的问题）。
			Before: func(ctx context.Context, id int64) error {
				e, err := repo.Get(ctx, id)
				if err != nil {
					return err
				}
				if e.IsDeleted() {
					return errors.NewCode(errors.FailedPrecondition, "entity not active")
				}
				return nil
			},
			After: func(_ context.Context, _ int64) error {
				deleteAfterCalls++
				return nil
			},
		},
		Purge: appcrud.Stage[int64]{
			Before: func(_ context.Context, _ int64) error {
				purgeBeforeCalls++
				return nil
			},
			After: func(_ context.Context, _ int64) error {
				purgeAfterCalls++
				return nil
			},
		},
	}})
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	// 先软删（触发 Delete 钩子），再 Purge 冷数据。
	repo.items[1] = &auditedTestEntity{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1}}}
	ctx, err := contextx.WithOperator(context.Background(), "alice")
	if err != nil {
		t.Fatalf("WithOperator: %v", err)
	}
	if err := app.Delete(ctx, 1); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if deleteAfterCalls != 1 {
		t.Fatalf("expected delete.After to run once on soft delete, got %d", deleteAfterCalls)
	}

	if err := app.Purge(ctx, 1); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if deleteAfterCalls != 1 {
		t.Fatalf("expected delete.After NOT to re-run on purge, got %d calls", deleteAfterCalls)
	}
	if purgeBeforeCalls != 1 || purgeAfterCalls != 1 {
		t.Fatalf("expected purge hooks to run once each, got before=%d after=%d", purgeBeforeCalls, purgeAfterCalls)
	}
	if repo.purges != 1 {
		t.Fatalf("expected purges=1, got %d", repo.purges)
	}
}

// TestAuditedApplication_Purge_FallsBackToDeleteHooks 验证未配置 Purge 钩子时
// 回退 Delete 钩子（兼容既有行为）。
func TestAuditedApplication_Purge_FallsBackToDeleteHooks(t *testing.T) {
	repo := newMemAuditedRepo()
	store := &recordingAuditStore{}

	deleteAfterCalls := 0
	app, err := NewApplication(repo, store, &appcrud.Options[*auditedTestEntity, int64]{Hooks: &appcrud.Hooks[*auditedTestEntity, int64]{
		Delete: appcrud.Stage[int64]{
			After: func(_ context.Context, _ int64) error {
				deleteAfterCalls++
				return nil
			},
		},
	}})
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	repo.items[1] = &auditedTestEntity{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1}}}
	ctx, err := contextx.WithOperator(context.Background(), "alice")
	if err != nil {
		t.Fatalf("WithOperator: %v", err)
	}
	if err := app.Purge(ctx, 1); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if deleteAfterCalls != 1 {
		t.Fatalf("expected fallback to delete.After on purge, got %d calls", deleteAfterCalls)
	}
}

// TestAuditedApplication_Restore_UsesDedicatedRestoreHooks 验证配置了 Restore 独立钩子时，
// 恢复走 Restore Stage 而不是重放 Update 钩子（P2-085）。
func TestAuditedApplication_Restore_UsesDedicatedRestoreHooks(t *testing.T) {
	repo := newMemAuditedRepo()
	store := &recordingAuditStore{}

	updateAfterCalls := 0
	restoreAfterCalls := 0
	app, err := NewApplication(repo, store, &appcrud.Options[*auditedTestEntity, int64]{Hooks: &appcrud.Hooks[*auditedTestEntity, int64]{
		Update: appcrud.Stage[*auditedTestEntity]{
			After: func(_ context.Context, _ *auditedTestEntity) error {
				updateAfterCalls++
				return nil
			},
		},
		Restore: appcrud.Stage[*auditedTestEntity]{
			After: func(_ context.Context, _ *auditedTestEntity) error {
				restoreAfterCalls++
				return nil
			},
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
	if updateAfterCalls != 0 {
		t.Fatalf("expected update.After NOT to run on restore, got %d calls", updateAfterCalls)
	}
	if restoreAfterCalls != 1 {
		t.Fatalf("expected restore.After to run once, got %d", restoreAfterCalls)
	}
}

// TestAuditedApplication_Restore_FallsBackToUpdateHooks 验证未配置 Restore 钩子时
// 回退 Update 钩子（兼容既有行为）。
func TestAuditedApplication_Restore_FallsBackToUpdateHooks(t *testing.T) {
	repo := newMemAuditedRepo()
	store := &recordingAuditStore{}

	updateAfterCalls := 0
	app, err := NewApplication(repo, store, &appcrud.Options[*auditedTestEntity, int64]{Hooks: &appcrud.Hooks[*auditedTestEntity, int64]{
		Update: appcrud.Stage[*auditedTestEntity]{
			After: func(_ context.Context, _ *auditedTestEntity) error {
				updateAfterCalls++
				return nil
			},
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
	if updateAfterCalls != 1 {
		t.Fatalf("expected fallback to update.After on restore, got %d calls", updateAfterCalls)
	}
}
