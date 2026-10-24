package crud

import (
	"context"
	"testing"

	"gochen/errors"
)

func TestApplication_Create_RollsBackWhenAfterHookFailsOnTransactionalRepo(t *testing.T) {
	repo := newBatchTestRepo()

	app, err := NewApplication[testEntity, int64](repo, &Options[testEntity, int64]{Config: DefaultServiceConfig(), Hooks: &Hooks[testEntity, int64]{
		Create: Stage[testEntity]{After: func(context.Context, testEntity) error {
			return errors.New("after create failed")
		}},
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

func TestApplication_Update_RollsBackWhenAfterHookFailsOnTransactionalRepo(t *testing.T) {
	repo := newBatchTestRepo()
	repo.existingIDs[1] = true

	app, err := NewApplication[testEntity, int64](repo, &Options[testEntity, int64]{Config: DefaultServiceConfig(), Hooks: &Hooks[testEntity, int64]{
		Update: Stage[testEntity]{After: func(context.Context, testEntity) error {
			return errors.New("after update failed")
		}},
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

func TestApplication_Delete_RollsBackWhenAfterHookFailsOnTransactionalRepo(t *testing.T) {
	repo := newBatchTestRepo()
	repo.existingIDs[1] = true

	app, err := NewApplication[testEntity, int64](repo, &Options[testEntity, int64]{Config: DefaultServiceConfig(), Hooks: &Hooks[testEntity, int64]{
		Delete: Stage[int64]{After: func(context.Context, int64) error {
			return errors.New("after delete failed")
		}},
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

	var afterSawCommitted bool
	var postCommitSawCommitted bool
	app, err := NewApplication[testEntity, int64](repo, &Options[testEntity, int64]{Config: DefaultServiceConfig(), Hooks: &Hooks[testEntity, int64]{
		Create: Stage[testEntity]{
			After: func(context.Context, testEntity) error {
				afterSawCommitted = len(repo.creates) > 0
				return nil
			},
			PostCommit: func(context.Context, testEntity) error {
				postCommitSawCommitted = len(repo.creates) > 0
				return nil
			},
		},
	}})
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	if err := app.Create(context.Background(), testEntity{id: 1}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if afterSawCommitted {
		t.Fatalf("expected After hook to run before commit")
	}
	if !postCommitSawCommitted {
		t.Fatalf("expected PostCommit hook to run after commit")
	}
}

func TestApplication_Create_ReturnsPostCommitErrorWithoutRollback(t *testing.T) {
	repo := newBatchTestRepo()

	app, err := NewApplication[testEntity, int64](repo, &Options[testEntity, int64]{Config: DefaultServiceConfig(), Hooks: &Hooks[testEntity, int64]{
		Create: Stage[testEntity]{PostCommit: func(context.Context, testEntity) error {
			return errors.New("post commit failed")
		}},
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
		Create: Stage[testEntity]{PostCommit: func(context.Context, testEntity) error {
			return nil
		}},
	}})
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	err = app.Create(context.Background(), testEntity{id: 1})
	if !errors.Is(err, errors.Unsupported) {
		t.Fatalf("expected Unsupported when post-commit hook is used without transaction support, got %v", err)
	}
}

func TestApplication_ActionHooksTriggerIndependently(t *testing.T) {
	repo := newBatchTestRepo()
	repo.existingIDs[1] = true

	var createCalls, updateCalls, deleteCalls int
	recordCreate := func(context.Context, testEntity) error {
		createCalls++
		return nil
	}
	recordUpdate := func(context.Context, testEntity) error {
		updateCalls++
		return nil
	}
	recordDelete := func(context.Context, int64) error {
		deleteCalls++
		return nil
	}
	app, err := NewApplication[testEntity, int64](repo, &Options[testEntity, int64]{Config: DefaultServiceConfig(), Hooks: &Hooks[testEntity, int64]{
		Create: Stage[testEntity]{Before: recordCreate, After: recordCreate, PostCommit: recordCreate},
		Update: Stage[testEntity]{Before: recordUpdate, After: recordUpdate, PostCommit: recordUpdate},
		Delete: Stage[int64]{Before: recordDelete, After: recordDelete, PostCommit: recordDelete},
	}})
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	if err := app.Create(context.Background(), testEntity{id: 2}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if createCalls != 3 || updateCalls != 0 || deleteCalls != 0 {
		t.Fatalf("after create: create=%d, update=%d, delete=%d", createCalls, updateCalls, deleteCalls)
	}

	if err := app.Update(context.Background(), testEntity{id: 1}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if createCalls != 3 || updateCalls != 3 || deleteCalls != 0 {
		t.Fatalf("after update: create=%d, update=%d, delete=%d", createCalls, updateCalls, deleteCalls)
	}

	if err := app.Delete(context.Background(), 1); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if createCalls != 3 || updateCalls != 3 || deleteCalls != 3 {
		t.Fatalf("after delete: create=%d, update=%d, delete=%d", createCalls, updateCalls, deleteCalls)
	}
}
