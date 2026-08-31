package tx

import (
	"context"

	"gochen/contextx"
	"gochen/errors"
)

// ITxLifecycleRunner 提供标准事务生命周期 primitive。
type ITxLifecycleRunner interface {
	BeginTx(ctx context.Context) (TxScope, error)
	Commit(tx TxScope) error
	Rollback(tx TxScope) error
}

// RunTxLifecycle 统一执行 begin -> fn -> commit -> after-commit，失败时自动 rollback。
func RunTxLifecycle(ctx context.Context, runner ITxLifecycleRunner, fn func(txCtx context.Context) error) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if runner == nil {
		return errors.NewCode(errors.InvalidInput, "tx lifecycle runner is nil")
	}
	if fn == nil {
		return errors.NewCode(errors.InvalidInput, "fn is nil")
	}

	txScope, err := runner.BeginTx(ctx)
	if err != nil {
		return err
	}

	committed := false
	defer func() {
		if committed {
			return
		}
		_ = runner.Rollback(txScope)
	}()

	txCtx := txScope.Context()
	if txCtx == nil {
		return errors.NewCode(errors.Internal, "BeginTx returned nil txCtx")
	}
	owned, ok := contextx.TxLifecycleFromContext(txCtx)
	if !ok {
		return errors.NewCode(errors.Internal, "BeginTx returned txCtx without transaction lifecycle metadata")
	}
	if owned != txScope.Owned() {
		return errors.NewCode(errors.Internal, "BeginTx returned mismatched transaction owner metadata")
	}

	if err := fn(txCtx); err != nil {
		return err
	}
	if err := runner.Commit(txScope); err != nil {
		if contextx.IsAfterCommitError(err) {
			committed = true
		}
		return err
	}

	committed = true
	if err := contextx.RunAfterCommit(txCtx); err != nil {
		return contextx.WrapAfterCommitError(err)
	}
	return nil
}
