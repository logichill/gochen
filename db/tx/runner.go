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
		// 回滚路径：先清空未执行的提交后回调。owner=true 封存整个事务，
		// owner=false 只撤销本内层作用域注册的回调（失败在下方显式处理）。
		contextx.ResetAfterCommit(txScope.Context())
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
		// 业务 fn 失败：本作用域注册的回调必须全部作废（owner=false 只回退
		// 内层新增部分，owner=true 封存整个事务），否则外层捕获错误继续提交时，
		// 已回滚逻辑的回调仍会被执行。
		contextx.ResetAfterCommit(txCtx)
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
