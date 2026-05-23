package crud

import (
	"context"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
	"gochen/contextx"
	"gochen/errors"
)

type fakeTxRepo struct {
	beginCalls    int
	commitCalls   int
	rollbackCalls int
	commitErr     error
}

func (r *fakeTxRepo) WithinTx(ctx context.Context, fn func(txCtx context.Context) error) error {
	return runWithTxLifecycle(ctx, r, fn)
}

func (r *fakeTxRepo) BeginTx(ctx context.Context) (contextx.TxScope, error) {
	r.beginCalls++
	return contextx.NewTxScope(ctx, true)
}

func (r *fakeTxRepo) Commit(tx contextx.TxScope) error {
	_ = tx
	r.commitCalls++
	return r.commitErr
}

func (r *fakeTxRepo) Rollback(tx contextx.TxScope) error {
	_ = tx
	r.rollbackCalls++
	return nil
}

func TestWithTx_CommitsOnSuccess(t *testing.T) {
	repo := &fakeTxRepo{}
	err := WithTx(context.Background(), repo, func(txCtx context.Context) error {
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 1, repo.beginCalls)
	require.Equal(t, 1, repo.commitCalls)
	require.Equal(t, 0, repo.rollbackCalls)
}

func TestWithTx_RollsBackOnFnError(t *testing.T) {
	repo := &fakeTxRepo{}
	want := errors.New("boom")
	err := WithTx(context.Background(), repo, func(txCtx context.Context) error {
		return want
	})
	require.ErrorIs(t, err, want)
	require.Equal(t, 1, repo.beginCalls)
	require.Equal(t, 0, repo.commitCalls)
	require.Equal(t, 1, repo.rollbackCalls)
}

func TestWithTx_RollsBackOnCommitError(t *testing.T) {
	want := errors.New("commit failed")
	repo := &fakeTxRepo{commitErr: want}
	err := WithTx(context.Background(), repo, func(txCtx context.Context) error {
		return nil
	})
	require.ErrorIs(t, err, want)
	require.Equal(t, 1, repo.beginCalls)
	require.Equal(t, 1, repo.commitCalls)
	require.Equal(t, 1, repo.rollbackCalls)
}

func TestWithTx_DoesNotRollbackOnAfterCommitError(t *testing.T) {
	want := errors.New("post commit failed")
	repo := &fakeTxRepo{commitErr: contextx.WrapAfterCommitError(want)}
	err := WithTx(context.Background(), repo, func(txCtx context.Context) error {
		return nil
	})
	require.ErrorIs(t, err, want)
	require.True(t, contextx.IsAfterCommitError(err))
	require.Equal(t, 1, repo.beginCalls)
	require.Equal(t, 1, repo.commitCalls)
	require.Equal(t, 0, repo.rollbackCalls)
}

func TestWithTx_NilTxRepoIsInvalidInput(t *testing.T) {
	err := WithTx(context.Background(), nil, func(txCtx context.Context) error {
		return nil
	})
	require.True(t, errors.Is(err, errors.InvalidInput))
}

func TestWithTx_NilFnIsInvalidInput(t *testing.T) {
	repo := &fakeTxRepo{}
	err := WithTx(context.Background(), repo, nil)
	require.True(t, errors.Is(err, errors.InvalidInput))
}

func TestWithTx_NilCtxIsInvalidInput(t *testing.T) {
	repo := &fakeTxRepo{}
	err := WithTx(nil, repo, func(txCtx context.Context) error {
		return nil
	})
	require.True(t, errors.Is(err, errors.InvalidInput))
}

type fakeTxRepoNilCtx struct{ rollbackCalls int }

func (r *fakeTxRepoNilCtx) WithinTx(ctx context.Context, fn func(txCtx context.Context) error) error {
	return runWithTxLifecycle(ctx, r, fn)
}

func (r *fakeTxRepoNilCtx) BeginTx(ctx context.Context) (contextx.TxScope, error) {
	return contextx.TxScope{}, nil
}
func (r *fakeTxRepoNilCtx) Commit(tx contextx.TxScope) error { return nil }
func (r *fakeTxRepoNilCtx) Rollback(tx contextx.TxScope) error {
	_ = tx
	r.rollbackCalls++
	return nil
}

func TestWithTx_BeginTxReturnsNilCtxIsInternalError(t *testing.T) {
	repo := &fakeTxRepoNilCtx{}
	err := WithTx(context.Background(), repo, func(txCtx context.Context) error {
		return nil
	})
	require.True(t, errors.Is(err, errors.Internal))
	require.Equal(t, 1, repo.rollbackCalls)
}

type fakeTxRepoMissingLifecycle struct{ rollbackCalls int }

type txScopeMirror struct {
	ctx   context.Context
	owned bool
}

func newUncheckedTxScope(ctx context.Context, owned bool) contextx.TxScope {
	raw := txScopeMirror{ctx: ctx, owned: owned}
	return *(*contextx.TxScope)(unsafe.Pointer(&raw))
}

func (r *fakeTxRepoMissingLifecycle) WithinTx(ctx context.Context, fn func(txCtx context.Context) error) error {
	return runWithTxLifecycle(ctx, r, fn)
}

func (r *fakeTxRepoMissingLifecycle) BeginTx(ctx context.Context) (contextx.TxScope, error) {
	return newUncheckedTxScope(context.WithValue(ctx, struct{}{}, true), true), nil
}
func (r *fakeTxRepoMissingLifecycle) Commit(tx contextx.TxScope) error { return nil }
func (r *fakeTxRepoMissingLifecycle) Rollback(tx contextx.TxScope) error {
	_ = tx
	r.rollbackCalls++
	return nil
}

func TestWithTx_BeginTxWithoutLifecycleIsInternalError(t *testing.T) {
	repo := &fakeTxRepoMissingLifecycle{}
	err := WithTx(context.Background(), repo, func(txCtx context.Context) error {
		return nil
	})
	require.True(t, errors.Is(err, errors.Internal))
	require.Equal(t, 1, repo.rollbackCalls)
}

type nestedAwareTxRepo struct {
	outerCommits  int
	rollbackCalls int
}

type nestedAwareTxKey struct{}

func (r *nestedAwareTxRepo) WithinTx(ctx context.Context, fn func(txCtx context.Context) error) error {
	return runWithTxLifecycle(ctx, r, fn)
}

func (r *nestedAwareTxRepo) BeginTx(ctx context.Context) (contextx.TxScope, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if started, _ := ctx.Value(nestedAwareTxKey{}).(bool); started {
		ctx = context.WithValue(ctx, nestedAwareTxKey{}, true)
		return contextx.NewTxScope(ctx, false)
	}
	ctx = context.WithValue(ctx, nestedAwareTxKey{}, true)
	return contextx.NewTxScope(ctx, true)
}

func (r *nestedAwareTxRepo) Commit(tx contextx.TxScope) error {
	owned, ok := contextx.TxLifecycleFromContext(tx.Context())
	if ok && owned {
		r.outerCommits++
	}
	return nil
}

func (r *nestedAwareTxRepo) Rollback(tx contextx.TxScope) error {
	_ = tx
	r.rollbackCalls++
	return nil
}

type fakeTxRepoOwnerMismatch struct{ rollbackCalls int }

func (r *fakeTxRepoOwnerMismatch) WithinTx(ctx context.Context, fn func(txCtx context.Context) error) error {
	return runWithTxLifecycle(ctx, r, fn)
}

func (r *fakeTxRepoOwnerMismatch) BeginTx(ctx context.Context) (contextx.TxScope, error) {
	txCtx, err := contextx.WithTxLifecycle(ctx, true)
	if err != nil {
		return contextx.TxScope{}, err
	}
	return newUncheckedTxScope(txCtx, false), nil
}
func (r *fakeTxRepoOwnerMismatch) Commit(tx contextx.TxScope) error { return nil }
func (r *fakeTxRepoOwnerMismatch) Rollback(tx contextx.TxScope) error {
	_ = tx
	r.rollbackCalls++
	return nil
}

func TestWithTx_BeginTxOwnerMismatchRollsBack(t *testing.T) {
	repo := &fakeTxRepoOwnerMismatch{}
	err := WithTx(context.Background(), repo, func(txCtx context.Context) error {
		return nil
	})
	require.True(t, errors.Is(err, errors.Internal))
	require.Equal(t, 1, repo.rollbackCalls)
}

func TestWithTx_WrapsRunAfterCommitError(t *testing.T) {
	repo := &fakeTxRepo{}
	want := errors.New("post commit failed")
	err := WithTx(context.Background(), repo, func(txCtx context.Context) error {
		return contextx.AppendAfterCommit(txCtx, func(context.Context) error {
			return want
		})
	})
	require.ErrorIs(t, err, want)
	require.True(t, contextx.IsAfterCommitError(err))
	require.Equal(t, 1, repo.commitCalls)
	require.Equal(t, 0, repo.rollbackCalls)
}

func TestWithTx_RunsAfterCommitOnlyOnOutermostTransaction(t *testing.T) {
	repo := &nestedAwareTxRepo{}
	called := 0

	err := WithTx(context.Background(), repo, func(txCtx context.Context) error {
		return WithTx(txCtx, repo, func(innerTx context.Context) error {
			require.NoError(t, contextx.AppendAfterCommit(innerTx, func(context.Context) error {
				called++
				return nil
			}))
			require.Equal(t, 0, called)
			return nil
		})
	})

	require.NoError(t, err)
	require.Equal(t, 1, repo.outerCommits)
	require.Equal(t, 1, called)
}

func TestWithTx_DoesNotRunAfterCommitWhenOuterTransactionRollsBack(t *testing.T) {
	repo := &nestedAwareTxRepo{}
	called := 0
	want := errors.New("outer failed")

	err := WithTx(context.Background(), repo, func(txCtx context.Context) error {
		err := WithTx(txCtx, repo, func(innerTx context.Context) error {
			return contextx.AppendAfterCommit(innerTx, func(context.Context) error {
				called++
				return nil
			})
		})
		require.NoError(t, err)
		return want
	})

	require.ErrorIs(t, err, want)
	require.Equal(t, 0, repo.outerCommits)
	require.Equal(t, 0, called)
}
