package tx

import (
	"context"
	"errors"
	"testing"

	"gochen/contextx"
)

type mockTxRunner struct {
	beginErr    error
	commitErr   error
	rollbackErr error
	committed   bool
	rolledBack  bool
	owned       bool
}

func (m *mockTxRunner) BeginTx(ctx context.Context) (TxScope, error) {
	if m.beginErr != nil {
		return TxScope{}, m.beginErr
	}
	return NewTxScope(ctx, m.owned)
}

func (m *mockTxRunner) Commit(tx TxScope) error {
	m.committed = true
	return m.commitErr
}

func (m *mockTxRunner) Rollback(tx TxScope) error {
	m.rolledBack = true
	return m.rollbackErr
}

func TestRunTxLifecycle_SuccessAndAfterCommit(t *testing.T) {
	runner := &mockTxRunner{owned: true}
	afterCommitRan := false

	err := RunTxLifecycle(context.Background(), runner, func(txCtx context.Context) error {
		return contextx.AppendAfterCommit(txCtx, func(ctx context.Context) error {
			afterCommitRan = true
			return nil
		})
	})

	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !runner.committed {
		t.Fatal("expected commit to be called")
	}
	if runner.rolledBack {
		t.Fatal("expected rollback not to be called")
	}
	if !afterCommitRan {
		t.Fatal("expected after-commit callback to run")
	}
}

func TestRunTxLifecycle_FnErrorTriggersRollback(t *testing.T) {
	runner := &mockTxRunner{owned: true}
	fnErr := errors.New("business logic failed")

	err := RunTxLifecycle(context.Background(), runner, func(txCtx context.Context) error {
		return fnErr
	})

	if !errors.Is(err, fnErr) {
		t.Fatalf("expected fn error, got %v", err)
	}
	if runner.committed {
		t.Fatal("expected commit not to be called on fn error")
	}
	if !runner.rolledBack {
		t.Fatal("expected rollback to be called on fn error")
	}
}

func TestRunTxLifecycle_NilArgs(t *testing.T) {
	runner := &mockTxRunner{owned: true}
	if err := RunTxLifecycle(nil, runner, func(ctx context.Context) error { return nil }); err == nil {
		t.Fatal("expected error on nil ctx")
	}
	if err := RunTxLifecycle(context.Background(), nil, func(ctx context.Context) error { return nil }); err == nil {
		t.Fatal("expected error on nil runner")
	}
	if err := RunTxLifecycle(context.Background(), runner, nil); err == nil {
		t.Fatal("expected error on nil fn")
	}
}
