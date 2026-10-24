package crud

import (
	"context"

	"testing"

	"gochen/errors"
)

// TestApplication_CreateAll_AllOrNothing_ValidationFailure 验证 Application CreateAll AllOrNothing ValidationFailure。
func TestApplication_CreateAll_AllOrNothing_ValidationFailure(t *testing.T) {
	repo := newBatchTestRepo()
	validator := &selectiveValidator{failIDs: map[int64]bool{2: true}}
	cfg := DefaultServiceConfig()
	cfg.AutoValidate = true

	app, err := NewApplication(repo, &Options[testEntity, int64]{Validator: validator, Config: cfg})
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	entities := []testEntity{
		{id: 1},
		{id: 2}, // 校验失败
		{id: 3},
	}

	err = app.CreateAll(context.Background(), entities)
	if err == nil {
		t.Fatalf("expected error when validation fails, got nil")
	}

	// 验证没有任何实体被创建（全回滚）
	if len(repo.creates) != 0 {
		t.Fatalf("expected 0 creates (all-or-nothing), got %d", len(repo.creates))
	}
}

// TestApplication_CreateAll_AllOrNothing_Success 验证 Application CreateAll AllOrNothing Success。
func TestApplication_CreateAll_AllOrNothing_Success(t *testing.T) {
	repo := newBatchTestRepo()
	app, err := NewApplication(repo, &Options[testEntity, int64]{Config: DefaultServiceConfig()})
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	entities := []testEntity{{id: 1}, {id: 2}, {id: 3}}

	err = app.CreateAll(context.Background(), entities)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证所有实体都被创建
	if len(repo.creates) != 3 {
		t.Fatalf("expected 3 creates, got %d", len(repo.creates))
	}
}

func TestApplication_CreateAll_RunsBeforeAndValidateOncePerEntity(t *testing.T) {
	repo := newBatchTestRepo()
	validator := &trackingValidator{}

	var beforeCalls int
	app, err := NewApplication(repo, &Options[testEntity, int64]{Validator: validator, Config: DefaultServiceConfig(), Hooks: &Hooks[testEntity, int64]{
		Create: Stage[testEntity]{Before: func(_ context.Context, _ testEntity) error {
			beforeCalls++
			return nil
		}},
	}})
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	err = app.CreateAll(context.Background(), []testEntity{{id: 1}, {id: 2}})
	if err != nil {
		t.Fatalf("create all: %v", err)
	}
	if beforeCalls != 2 {
		t.Fatalf("expected Before hook once per entity, got %d", beforeCalls)
	}
	if validator.called != 2 {
		t.Fatalf("expected Validate once per entity, got %d", validator.called)
	}
}

func TestApplication_UpdateAll_RunsBeforeAndValidateOncePerEntity(t *testing.T) {
	repo := newBatchTestRepo()
	repo.existingIDs[1] = true
	repo.existingIDs[2] = true
	validator := &trackingValidator{}

	var beforeCalls int
	app, err := NewApplication(repo, &Options[testEntity, int64]{Validator: validator, Config: DefaultServiceConfig(), Hooks: &Hooks[testEntity, int64]{
		Update: Stage[testEntity]{Before: func(_ context.Context, _ testEntity) error {
			beforeCalls++
			return nil
		}},
	}})
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	err = app.UpdateAll(context.Background(), []testEntity{{id: 1}, {id: 2}})
	if err != nil {
		t.Fatalf("update all: %v", err)
	}
	if beforeCalls != 2 {
		t.Fatalf("expected Before hook once per entity, got %d", beforeCalls)
	}
	if validator.called != 2 {
		t.Fatalf("expected Validate once per entity, got %d", validator.called)
	}
}

func TestApplication_DeleteAll_RunsBeforeOncePerID(t *testing.T) {
	repo := newBatchTestRepo()
	repo.existingIDs[1] = true
	repo.existingIDs[2] = true

	var beforeCalls int
	app, err := NewApplication[testEntity, int64](repo, &Options[testEntity, int64]{Config: DefaultServiceConfig(), Hooks: &Hooks[testEntity, int64]{
		Delete: Stage[int64]{Before: func(_ context.Context, _ int64) error {
			beforeCalls++
			return nil
		}},
	}})
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	err = app.DeleteAll(context.Background(), []int64{1, 2})
	if err != nil {
		t.Fatalf("delete all: %v", err)
	}
	if beforeCalls != 2 {
		t.Fatalf("expected Before hook once per id, got %d", beforeCalls)
	}
}

func TestApplication_CreateAll_AfterHookRunsBeforeCommitAndPostCommitRunsAfterCommit(t *testing.T) {
	repo := newBatchTestRepo()

	var afterCreateSawCommitted bool
	var postCommitSawCommitted bool
	app, err := NewApplication(repo, &Options[testEntity, int64]{Config: DefaultServiceConfig(), Hooks: &Hooks[testEntity, int64]{
		Create: Stage[testEntity]{
			After: func(_ context.Context, entity testEntity) error {
				if len(repo.creates) > 0 {
					afterCreateSawCommitted = true
				}
				return nil
			},
			PostCommit: func(_ context.Context, entity testEntity) error {
				if len(repo.creates) > 0 {
					postCommitSawCommitted = true
				}
				return nil
			},
		},
	}})
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	if err := app.CreateAll(context.Background(), []testEntity{{id: 1}, {id: 2}}); err != nil {
		t.Fatalf("create all: %v", err)
	}
	if afterCreateSawCommitted {
		t.Fatalf("expected After hook to run before commit in batch path")
	}
	if !postCommitSawCommitted {
		t.Fatalf("expected PostCommit hook to run after commit in batch path")
	}
}

func TestApplication_CreateAll_RollsBackWhenAfterCreateFails(t *testing.T) {
	repo := newBatchTestRepo()

	var afterCalls int
	app, err := NewApplication(repo, &Options[testEntity, int64]{Config: DefaultServiceConfig(), Hooks: &Hooks[testEntity, int64]{
		Create: Stage[testEntity]{After: func(_ context.Context, entity testEntity) error {
			afterCalls++
			if entity.id == 2 {
				return errors.New("after create failed")
			}
			return nil
		}},
	}})
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	err = app.CreateAll(context.Background(), []testEntity{{id: 1}, {id: 2}})
	if err == nil {
		t.Fatalf("expected after create failure")
	}
	if afterCalls != 2 {
		t.Fatalf("expected after hook to run for each created entity before failure, got %d", afterCalls)
	}
	if len(repo.creates) != 0 {
		t.Fatalf("expected batch create to rollback when after hook fails, got %+v", repo.creates)
	}
}

// TestApplication_DeleteAll_AllOrNothing_NotFoundFails 验证 Application DeleteAll AllOrNothing NotFoundFails。
func TestApplication_DeleteAll_AllOrNothing_NotFoundFails(t *testing.T) {
	repo := newBatchTestRepo()
	// 预置实体 1, 3
	repo.existingIDs[1] = true
	repo.existingIDs[3] = true

	app, err := NewApplication(repo, &Options[testEntity, int64]{Config: DefaultServiceConfig()})
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	// 尝试删除 1, 2, 3（其中 2 不存在）
	err = app.DeleteAll(context.Background(), []int64{1, 2, 3})
	if err == nil {
		t.Fatalf("expected error when entity not found, got nil")
	}
	if !errors.Is(err, errors.NotFound) {
		t.Fatalf("expected NotFound error, got: %v", err)
	}

	// 验证事务回滚：不应删除任何已存在实体
	if !repo.existingIDs[1] || !repo.existingIDs[3] {
		t.Fatalf("expected rollback to keep entities 1 and 3, got %+v", repo.existingIDs)
	}
}

// TestApplication_DeleteAll_AllOrNothing_Success 验证 Application DeleteAll AllOrNothing Success。
func TestApplication_DeleteAll_AllOrNothing_Success(t *testing.T) {
	repo := newBatchTestRepo()
	repo.existingIDs[1] = true
	repo.existingIDs[2] = true
	repo.existingIDs[3] = true

	app, err := NewApplication(repo, &Options[testEntity, int64]{Config: DefaultServiceConfig()})
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	err = app.DeleteAll(context.Background(), []int64{1, 2, 3})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证所有实体都被删除
	if len(repo.existingIDs) != 0 {
		t.Fatalf("expected all entities deleted, got %d remaining", len(repo.existingIDs))
	}
}

// TestApplication_CreateAll_ExceedsMaxBatchSize 验证 Application CreateAll ExceedsMaxBatchSize。
func TestApplication_CreateAll_ExceedsMaxBatchSize(t *testing.T) {
	repo := newBatchTestRepo()
	cfg := DefaultServiceConfig()
	cfg.MaxBatchSize = 3

	app, err := NewApplication(repo, &Options[testEntity, int64]{Config: cfg})
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	entities := []testEntity{{id: 1}, {id: 2}, {id: 3}, {id: 4}}
	err = app.CreateAll(context.Background(), entities)
	if !errors.Is(err, errors.Validation) {
		t.Fatalf("expected Validation error for exceeding max batch size, got: %v", err)
	}
}

// TestApplication_DeleteAll_ExceedsMaxBatchSize 验证 Application DeleteAll ExceedsMaxBatchSize。
func TestApplication_DeleteAll_ExceedsMaxBatchSize(t *testing.T) {
	repo := newBatchTestRepo()
	cfg := DefaultServiceConfig()
	cfg.MaxBatchSize = 3

	app, err := NewApplication(repo, &Options[testEntity, int64]{Config: cfg})
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	err = app.DeleteAll(context.Background(), []int64{1, 2, 3, 4})
	if !errors.Is(err, errors.Validation) {
		t.Fatalf("expected Validation error for exceeding max batch size, got: %v", err)
	}
}

// TestApplication_CreateAll_EmptyListReturnsNil 验证 Application CreateAll EmptyListReturnsNil。
func TestApplication_CreateAll_EmptyListReturnsNil(t *testing.T) {
	repo := newBatchTestRepo()
	app, err := NewApplication(repo, &Options[testEntity, int64]{Config: DefaultServiceConfig()})
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	err = app.CreateAll(context.Background(), []testEntity{})
	if err != nil {
		t.Fatalf("expected nil error for empty list, got: %v", err)
	}
}

// TestApplication_DeleteAll_EmptyListReturnsNil 验证 Application DeleteAll EmptyListReturnsNil。
func TestApplication_DeleteAll_EmptyListReturnsNil(t *testing.T) {
	repo := newBatchTestRepo()
	app, err := NewApplication(repo, &Options[testEntity, int64]{Config: DefaultServiceConfig()})
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	err = app.DeleteAll(context.Background(), []int64{})
	if err != nil {
		t.Fatalf("expected nil error for empty list, got: %v", err)
	}
}

// TestApplication_DeleteAll_DuplicateIDsIdempotent 验证 DeleteAll 对重复 ID 幂等去重。
func TestApplication_DeleteAll_DuplicateIDsIdempotent(t *testing.T) {
	repo := newBatchTestRepo()
	repo.existingIDs[1] = true
	repo.existingIDs[2] = true

	app, err := NewApplication(repo, &Options[testEntity, int64]{Config: DefaultServiceConfig()})
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	err = app.DeleteAll(context.Background(), []int64{1, 1, 2})
	if err != nil {
		t.Fatalf("expected DeleteAll to succeed with duplicate IDs, got: %v", err)
	}

	if repo.existingIDs[1] || repo.existingIDs[2] {
		t.Fatalf("expected entities 1 and 2 to be deleted, got: %+v", repo.existingIDs)
	}
}

// TestApplication_UpdateAll_RejectsDuplicateIDs 验证 UpdateAll 拦截同批次重复实体 ID。
func TestApplication_UpdateAll_RejectsDuplicateIDs(t *testing.T) {
	repo := newBatchTestRepo()
	app, err := NewApplication(repo, &Options[testEntity, int64]{Config: DefaultServiceConfig()})
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	entities := []testEntity{{id: 1}, {id: 2}, {id: 1}}
	err = app.UpdateAll(context.Background(), entities)
	if err == nil {
		t.Fatal("expected UpdateAll to reject duplicate entity IDs, got nil")
	}
	if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected InvalidInput error, got %v", err)
	}
}
