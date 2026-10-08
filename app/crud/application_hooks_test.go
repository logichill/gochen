package crud

import (
	"context"

	"testing"

	"gochen/errors"
)

func TestApplication_Create_RollsBackWhenAfterCreateFailsOnTransactionalRepo(t *testing.T) {
	repo := newBatchTestRepo()

	app, err := NewApplication[testEntity, int64](repo, &Options[testEntity, int64]{Config: DefaultServiceConfig(), Hooks: &Hooks[testEntity, int64]{
		AfterCreate: func(ctx context.Context, entity testEntity) error {
			return errors.New("after create failed")
		},
	}})
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	err = app.Create(context.Background(), testEntity{id: 1})
	if err == nil {
		t.Fatalf("expected error when after create hook fails")
	}
	exists, err := repo.Exists(context.Background(), 1)
	if err != nil {
		t.Fatalf("exists: %v", err)
	}
	if exists {
		t.Fatalf("expected create to rollback when after hook fails")
	}
	if len(repo.creates) != 0 {
		t.Fatalf("expected no committed creates, got %+v", repo.creates)
	}
}

func TestApplication_Update_RollsBackWhenAfterUpdateFailsOnTransactionalRepo(t *testing.T) {
	repo := newBatchTestRepo()
	repo.existingIDs[1] = true

	app, err := NewApplication[testEntity, int64](repo, &Options[testEntity, int64]{Config: DefaultServiceConfig(), Hooks: &Hooks[testEntity, int64]{
		AfterUpdate: func(ctx context.Context, entity testEntity) error {
			return errors.New("after update failed")
		},
	}})
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	err = app.Update(context.Background(), testEntity{id: 1})
	if err == nil {
		t.Fatalf("expected error when after update hook fails")
	}
	if len(repo.updates) != 0 {
		t.Fatalf("expected no committed updates, got %+v", repo.updates)
	}
	exists, err := repo.Exists(context.Background(), 1)
	if err != nil {
		t.Fatalf("exists: %v", err)
	}
	if !exists {
		t.Fatalf("expected entity to remain after update rollback")
	}
}

func TestApplication_Delete_RollsBackWhenAfterDeleteFailsOnTransactionalRepo(t *testing.T) {
	repo := newBatchTestRepo()
	repo.existingIDs[1] = true

	app, err := NewApplication[testEntity, int64](repo, &Options[testEntity, int64]{Config: DefaultServiceConfig(), Hooks: &Hooks[testEntity, int64]{
		AfterDelete: func(ctx context.Context, id int64) error {
			return errors.New("after delete failed")
		},
	}})
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	err = app.Delete(context.Background(), 1)
	if err == nil {
		t.Fatalf("expected error when after delete hook fails")
	}
	if len(repo.deletes) != 0 {
		t.Fatalf("expected no committed deletes, got %+v", repo.deletes)
	}
	exists, err := repo.Exists(context.Background(), 1)
	if err != nil {
		t.Fatalf("exists: %v", err)
	}
	if !exists {
		t.Fatalf("expected delete to rollback when after hook fails")
	}
}

func TestApplication_Create_PostCommitHookRunsAfterCommit(t *testing.T) {
	repo := newBatchTestRepo()

	var afterCreateSawCommitted bool
	var postCommitSawCommitted bool
	app, err := NewApplication[testEntity, int64](repo, &Options[testEntity, int64]{Config: DefaultServiceConfig(), Hooks: &Hooks[testEntity, int64]{
		AfterCreate: func(ctx context.Context, entity testEntity) error {
			afterCreateSawCommitted = len(repo.creates) > 0
			return nil
		},
		PostCommitCreate: func(ctx context.Context, entity testEntity) error {
			postCommitSawCommitted = len(repo.creates) > 0
			return nil
		},
	}})
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	if err := app.Create(context.Background(), testEntity{id: 1}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if afterCreateSawCommitted {
		t.Fatalf("expected AfterCreate to run before commit")
	}
	if !postCommitSawCommitted {
		t.Fatalf("expected PostCommitCreate to run after commit")
	}
}

func TestApplication_Create_ReturnsPostCommitErrorWithoutRollback(t *testing.T) {
	repo := newBatchTestRepo()

	app, err := NewApplication[testEntity, int64](repo, &Options[testEntity, int64]{Config: DefaultServiceConfig(), Hooks: &Hooks[testEntity, int64]{
		PostCommitCreate: func(ctx context.Context, entity testEntity) error {
			return errors.New("post commit failed")
		},
	}})
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	err = app.Create(context.Background(), testEntity{id: 1})
	if err == nil {
		t.Fatalf("expected post-commit error")
	}
	exists, err := repo.Exists(context.Background(), 1)
	if err != nil {
		t.Fatalf("exists: %v", err)
	}
	if !exists {
		t.Fatalf("expected create to remain committed when post-commit hook fails")
	}
}

func TestApplication_Create_PostCommitHookRequiresTransactionalRepository(t *testing.T) {
	repo := &recordingRepo{}

	app, err := NewApplication[testEntity, int64](repo, &Options[testEntity, int64]{Config: DefaultServiceConfig(), Hooks: &Hooks[testEntity, int64]{
		PostCommitCreate: func(ctx context.Context, entity testEntity) error {
			return nil
		},
	}})
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	err = app.Create(context.Background(), testEntity{id: 1})
	if !errors.Is(err, errors.Unsupported) {
		t.Fatalf("expected Unsupported when post-commit hook is used without transaction support, got %v", err)
	}
}
