package audited

import (
	"context"
	"encoding/json"
	"fmt"

	"testing"
	"time"

	appcrud "gochen/app/crud"
	"gochen/contextx"

	"gochen/domain/audited"
	"gochen/domain/crud"
	"gochen/errors"
)

// TestAuditedApplication_UpdateAll_UsesBatchAuditStoreWhenAvailable 验证 AuditedApplication UpdateAll UsesBatchAuditStoreWhenAvailable。
func TestAuditedApplication_UpdateAll_UsesBatchAuditStoreWhenAvailable(t *testing.T) {
	repo := newMemAuditedRepo()
	store := &recordingBatchAuditStore{}
	app, err := NewApplication(repo, store, nil)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	createdAt := time.Date(2024, 2, 3, 4, 5, 6, 0, time.UTC)
	// 预置实体
	repo.items[1] = &auditedTestEntity{
		AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1, Version: 1}, CreatedAt: createdAt, CreatedBy: "owner-1"},
		Name:          "a",
		Status:        "p",
	}
	repo.items[2] = &auditedTestEntity{
		AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 2, Version: 1}, CreatedAt: createdAt, CreatedBy: "owner-2"},
		Name:          "b",
		Status:        "p",
	}

	ctx, err := contextx.WithOperator(context.Background(), "alice")
	if err != nil {
		t.Fatalf("WithOperator returned error: %v", err)
	}
	updates := []*auditedTestEntity{
		{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1, Version: 1}, CreatedBy: "attacker", DeletedBy: func() *string { v := "attacker"; return &v }()}, Name: "a2", Status: "active"},
		{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 2, Version: 1}}, Name: "b2", Status: "active"},
	}
	if err := app.UpdateAll(ctx, updates); err != nil {
		t.Fatalf("UpdateAll: %v", err)
	}

	if store.batchCalls != 1 {
		t.Fatalf("expected batchCalls=1, got %d", store.batchCalls)
	}
	if store.singleCalls != 0 {
		t.Fatalf("expected singleCalls=0, got %d", store.singleCalls)
	}
	if store.lastBatchLen != 2 {
		t.Fatalf("expected lastBatchLen=2, got %d", store.lastBatchLen)
	}
	if len(store.records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(store.records))
	}
	if repo.items[1].CreatedBy != "owner-1" || !repo.items[1].CreatedAt.Equal(createdAt) {
		t.Fatalf("first update replaced managed created fields: %+v", repo.items[1].AuditedEntity)
	}
	if repo.items[2].CreatedBy != "owner-2" || !repo.items[2].CreatedAt.Equal(createdAt) {
		t.Fatalf("second update cleared managed created fields: %+v", repo.items[2].AuditedEntity)
	}
	if repo.items[1].DeletedBy != nil {
		t.Fatalf("first update replaced managed deleted_by: %+v", repo.items[1].AuditedEntity)
	}
	for _, rec := range store.records {
		if rec.Operation != audited.AuditOpUpdate {
			t.Fatalf("unexpected op: %+v", rec)
		}
		if rec.Operator != "alice" {
			t.Fatalf("unexpected operator: %+v", rec)
		}
		if rec.Changes == nil {
			t.Fatalf("expected changes, got nil: %+v", rec)
		}
	}
}

func TestAuditedApplication_UpdateAll_RollsBackWhenDiffMarshalFails(t *testing.T) {
	repo := newMemAuditedRepo()
	store := &recordingBatchAuditStore{}
	app, err := NewApplication(repo, store, nil)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	repo.items[1] = &auditedTestEntity{
		AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1, Version: 1}},
		Name:          "old-1",
	}
	repo.items[2] = &auditedTestEntity{
		AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 2, Version: 1}},
		Name:          "old-2",
	}

	ctx, err := contextx.WithOperator(context.Background(), "alice")
	if err != nil {
		t.Fatalf("WithOperator returned error: %v", err)
	}
	updates := []*auditedTestEntity{
		{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1, Version: 1}}, Name: "new-1"},
		{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 2, Version: 1}}, Name: "new-2", Snapshot: make(chan int)},
	}

	if err := app.UpdateAll(ctx, updates); !errors.Is(err, errors.Internal) {
		t.Fatalf("expected Internal when diff marshal fails, got %v", err)
	}
	got, err := repo.Get(context.Background(), 1)
	if err != nil {
		t.Fatalf("get first entity after failed update: %v", err)
	}
	if got.Name != "old-1" {
		t.Fatalf("expected first entity rollback, got name=%s", got.Name)
	}
	got, err = repo.Get(context.Background(), 2)
	if err != nil {
		t.Fatalf("get second entity after failed update: %v", err)
	}
	if got.Name != "old-2" {
		t.Fatalf("expected second entity rollback, got name=%s", got.Name)
	}
	if len(store.records) != 0 {
		t.Fatalf("expected no audit records after diff marshal failure, got %d", len(store.records))
	}
}

func TestAuditedApplication_CreateAll_AfterHookRunsBeforeCommitAndPostCommitRunsAfterCommit(t *testing.T) {
	repo := newMemAuditedRepo()
	store := &recordingAuditStore{}

	var afterCreateSawCommitted bool
	var postCommitSawCommitted bool
	app, err := NewApplication(repo, store, &appcrud.Options[*auditedTestEntity, int64]{Hooks: &appcrud.Hooks[*auditedTestEntity, int64]{
		Create: appcrud.Stage[*auditedTestEntity]{
			After: func(_ context.Context, entity *auditedTestEntity) error {
				_, afterCreateSawCommitted = repo.items[entity.GetID()]
				return nil
			},
			PostCommit: func(_ context.Context, entity *auditedTestEntity) error {
				_, postCommitSawCommitted = repo.items[entity.GetID()]
				return nil
			},
		},
	}})
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	ctx, err := contextx.WithOperator(context.Background(), "alice")
	if err != nil {
		t.Fatalf("WithOperator returned error: %v", err)
	}
	entities := []*auditedTestEntity{
		{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1}}},
		{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 2}}},
	}
	if err := app.CreateAll(ctx, entities); err != nil {
		t.Fatalf("CreateAll: %v", err)
	}
	if afterCreateSawCommitted {
		t.Fatalf("expected batch after hook to run before outer transaction commit")
	}
	if !postCommitSawCommitted {
		t.Fatalf("expected batch post-commit hook to observe outer transaction commit")
	}
}

func TestAuditedApplication_CreateAll_RollsBackWhenAfterCreateFails(t *testing.T) {
	repo := newMemAuditedRepo()
	store := &recordingAuditStore{}

	var afterCalls int
	app, err := NewApplication(repo, store, &appcrud.Options[*auditedTestEntity, int64]{Hooks: &appcrud.Hooks[*auditedTestEntity, int64]{
		Create: appcrud.Stage[*auditedTestEntity]{After: func(_ context.Context, entity *auditedTestEntity) error {
			afterCalls++
			if entity.GetID() == 2 {
				return errors.NewCode(errors.Internal, "after create failed")
			}
			return nil
		}},
	}})
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	ctx, err := contextx.WithOperator(context.Background(), "alice")
	if err != nil {
		t.Fatalf("WithOperator returned error: %v", err)
	}
	entities := []*auditedTestEntity{
		{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1}}},
		{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 2}}},
	}
	if err := app.CreateAll(ctx, entities); err == nil {
		t.Fatalf("expected CreateAll to fail when after hook fails")
	}
	if afterCalls != 2 {
		t.Fatalf("expected after hook to run for each entity before failure, got %d", afterCalls)
	}
	if _, err := repo.Get(context.Background(), 1); !errors.Is(err, errors.NotFound) {
		t.Fatalf("expected first entity not persisted after rollback, got err=%v", err)
	}
	if _, err := repo.Get(context.Background(), 2); !errors.Is(err, errors.NotFound) {
		t.Fatalf("expected second entity not persisted after rollback, got err=%v", err)
	}
	if len(store.records) != 0 {
		t.Fatalf("expected no audit records after rollback, got %d", len(store.records))
	}
}

func TestAuditedApplication_CreateAll_RollsBackWhenSnapshotMarshalFails(t *testing.T) {
	repo := newMemAuditedRepo()
	store := &recordingBatchAuditStore{}
	app, err := NewApplication(repo, store, nil)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	ctx, err := contextx.WithOperator(context.Background(), "alice")
	if err != nil {
		t.Fatalf("WithOperator returned error: %v", err)
	}
	entities := []*auditedTestEntity{
		{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1}}},
		{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 2}}, Snapshot: make(chan int)},
	}
	if err := app.CreateAll(ctx, entities); !errors.Is(err, errors.Internal) {
		t.Fatalf("expected Internal when snapshot marshal fails, got %v", err)
	}
	if _, err := repo.Get(context.Background(), 1); !errors.Is(err, errors.NotFound) {
		t.Fatalf("expected first entity not persisted after rollback, got err=%v", err)
	}
	if _, err := repo.Get(context.Background(), 2); !errors.Is(err, errors.NotFound) {
		t.Fatalf("expected second entity not persisted after rollback, got err=%v", err)
	}
	if len(store.records) != 0 {
		t.Fatalf("expected no audit records after snapshot marshal failure, got %d", len(store.records))
	}
}

func TestAuditedApplication_UpdateAll_PrefetchBeforeSnapshots_UsesListByIds(t *testing.T) {
	repo := &memAuditedRepoWithListByIds{memAuditedRepo: newMemAuditedRepo()}
	store := &recordingBatchAuditStore{}
	app, err := NewApplication(repo, store, nil)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	repo.items[1] = &auditedTestEntity{
		AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1, Version: 1}},
		Name:          "old",
		Status:        "pending",
	}
	repo.items[2] = &auditedTestEntity{
		AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 2, Version: 1}},
		Name:          "old",
		Status:        "pending",
	}

	ctx, err := contextx.WithOperator(context.Background(), "alice")
	if err != nil {
		t.Fatalf("WithOperator returned error: %v", err)
	}
	updates := []*auditedTestEntity{
		{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1, Version: 1}}, Name: "new", Status: "active"},
		{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 2, Version: 1}}, Name: "new", Status: "active"},
	}
	if err := app.UpdateAll(ctx, updates); err != nil {
		t.Fatalf("UpdateAll: %v", err)
	}
	if repo.getCalls != 0 {
		t.Fatalf("expected Get not called when ListByIds available, got %d", repo.getCalls)
	}
	if repo.listByIdsCalls != 1 {
		t.Fatalf("expected ListByIds called once, got %d", repo.listByIdsCalls)
	}
	if len(store.records) != 2 {
		t.Fatalf("expected 2 audit records, got %d", len(store.records))
	}
}

func TestAuditedApplication_UpdateAll_PrefetchBeforeSnapshots_ChunksListByIds(t *testing.T) {
	repo := &memAuditedRepoWithListByIds{memAuditedRepo: newMemAuditedRepo()}
	store := &recordingBatchAuditStore{}
	app, err := NewApplication(repo, store, nil)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	n := updateAllBeforeSnapshotChunkSize + 1
	for i := 0; i < n; i++ {
		id := int64(i + 1)
		repo.items[id] = &auditedTestEntity{
			AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: id, Version: 1}},
			Name:          fmt.Sprintf("old-%d", id),
			Status:        "pending",
		}
	}

	updates := make([]*auditedTestEntity, 0, n)
	for i := 0; i < n; i++ {
		id := int64(i + 1)
		updates = append(updates, &auditedTestEntity{
			AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: id, Version: 1}},
			Name:          fmt.Sprintf("new-%d", id),
			Status:        "active",
		})
	}

	ctx, err := contextx.WithOperator(context.Background(), "alice")
	if err != nil {
		t.Fatalf("WithOperator returned error: %v", err)
	}
	if err := app.UpdateAll(ctx, updates); err != nil {
		t.Fatalf("UpdateAll: %v", err)
	}
	if repo.getCalls != 0 {
		t.Fatalf("expected Get not called when ListByIds available, got %d", repo.getCalls)
	}
	if repo.listByIdsCalls != 2 {
		t.Fatalf("expected ListByIds called twice, got %d", repo.listByIdsCalls)
	}
	if len(store.records) != n {
		t.Fatalf("expected %d audit records, got %d", n, len(store.records))
	}
}

func TestAuditedApplication_UpdateAll_PrefetchBeforeSnapshots_FallsBackToGet(t *testing.T) {
	repo := &memAuditedRepoCountingGet{memAuditedRepo: newMemAuditedRepo()}
	store := &recordingBatchAuditStore{}
	app, err := NewApplication(repo, store, nil)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	repo.items[1] = &auditedTestEntity{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1, Version: 1}}, Name: "a", Status: "p"}
	repo.items[2] = &auditedTestEntity{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 2, Version: 1}}, Name: "b", Status: "p"}
	repo.items[3] = &auditedTestEntity{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 3, Version: 1}}, Name: "c", Status: "p"}

	ctx, err := contextx.WithOperator(context.Background(), "alice")
	if err != nil {
		t.Fatalf("WithOperator returned error: %v", err)
	}
	updates := []*auditedTestEntity{
		{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1, Version: 1}}, Name: "a2", Status: "active"},
		{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 2, Version: 1}}, Name: "b2", Status: "active"},
		{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 3, Version: 1}}, Name: "c2", Status: "active"},
	}
	if err := app.UpdateAll(ctx, updates); err != nil {
		t.Fatalf("UpdateAll: %v", err)
	}
	if repo.getCalls != len(updates) {
		t.Fatalf("expected Get called %d times, got %d", len(updates), repo.getCalls)
	}
}

func TestAuditedApplication_UpdateAll_PrefetchBeforeSnapshots_FillsMissingListByIdsEntriesFromGet(t *testing.T) {
	repo := &memAuditedRepoWithPartialListByIds{
		memAuditedRepoWithListByIds: &memAuditedRepoWithListByIds{memAuditedRepo: newMemAuditedRepo()},
		missing:                     map[int64]struct{}{2: {}},
	}
	store := &recordingBatchAuditStore{}
	app, err := NewApplication(repo, store, nil)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	repo.items[1] = &auditedTestEntity{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1, Version: 1}}, Name: "old-1", Status: "pending"}
	repo.items[2] = &auditedTestEntity{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 2, Version: 1}}, Name: "old-2", Status: "pending"}

	ctx, err := contextx.WithOperator(context.Background(), "alice")
	if err != nil {
		t.Fatalf("WithOperator returned error: %v", err)
	}
	updates := []*auditedTestEntity{
		{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1, Version: 1}}, Name: "new-1", Status: "active"},
		{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 2, Version: 1}}, Name: "new-2", Status: "active"},
	}
	if err := app.UpdateAll(ctx, updates); err != nil {
		t.Fatalf("UpdateAll: %v", err)
	}
	if repo.getCalls != 1 {
		t.Fatalf("expected Get called once for missing prefetch entity, got %d", repo.getCalls)
	}
	if repo.listByIdsCalls != 1 {
		t.Fatalf("expected ListByIds called once, got %d", repo.listByIdsCalls)
	}
	if len(store.records) != 2 {
		t.Fatalf("expected 2 audit records, got %d", len(store.records))
	}

	var diffs []audited.FieldChange
	if err := json.Unmarshal(store.records[1].Changes, &diffs); err != nil {
		t.Fatalf("unmarshal diff: %v", err)
	}
	if len(diffs) != 2 {
		t.Fatalf("expected 2 field changes for fallback-loaded entity, got %d: %s", len(diffs), string(store.records[1].Changes))
	}
}

// TestAuditedApplication_DeleteAll_DuplicateIDsIdempotent 验证当仓储过滤软删行时，
// DeleteAll 对重复 ID 幂等（与 repo 层 WHERE id IN ? 语义一致）。
func TestAuditedApplication_DeleteAll_DuplicateIDsIdempotent(t *testing.T) {
	repo := &memAuditedRepoFilteringDeleted{memAuditedRepo: newMemAuditedRepo()}
	store := &recordingAuditStore{}
	app, err := NewApplication(repo, store, nil)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	repo.items[1] = &auditedTestEntity{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1}}}
	repo.items[2] = &auditedTestEntity{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 2}}}

	ctx, err := contextx.WithOperator(context.Background(), "alice")
	if err != nil {
		t.Fatalf("WithOperator returned error: %v", err)
	}

	if err := app.DeleteAll(ctx, []int64{1, 1, 2}); err != nil {
		t.Fatalf("expected DeleteAll to be idempotent for duplicate ids, got %v", err)
	}
	if repo.updates != 2 {
		t.Fatalf("expected 2 updates for distinct ids, got %d", repo.updates)
	}
	if len(store.records) != 2 {
		t.Fatalf("expected 2 audit records for distinct ids, got %d", len(store.records))
	}
}

func TestAuditedApplication_DeleteAll_ValidatesRawBatchBeforeDedup(t *testing.T) {
	repo := &memAuditedRepoFilteringDeleted{memAuditedRepo: newMemAuditedRepo()}
	store := &recordingAuditStore{}
	cfg := appcrud.DefaultServiceConfig()
	cfg.MaxBatchSize = 2
	app, err := NewApplication(repo, store, &appcrud.Options[*auditedTestEntity, int64]{Config: cfg})
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	repo.items[1] = &auditedTestEntity{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1}}}
	ctx, err := contextx.WithOperator(context.Background(), "alice")
	if err != nil {
		t.Fatalf("WithOperator returned error: %v", err)
	}

	err = app.DeleteAll(ctx, []int64{1, 1, 1})
	if !errors.Is(err, errors.Validation) {
		t.Fatalf("expected Validation for raw batch size overflow, got %v", err)
	}
	if repo.updates != 0 {
		t.Fatalf("expected no updates after validation failure, got %d", repo.updates)
	}
	if len(store.records) != 0 {
		t.Fatalf("expected no audit records after validation failure, got %d", len(store.records))
	}
}
