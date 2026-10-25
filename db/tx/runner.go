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

	if err := runTxLifecycleFn(txCtx, txScope.Owned(), fn); err != nil {
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

// runTxLifecycleFn 执行业务函数，统一 error 与 panic 两种失败路径的语义：
//
//   - 返回 error：撤销本作用域注册的提交后回调；非 owner 作用域同时把事务
//     标记为 rollback-only，外层捕获错误继续提交时由 owner 在提交点拒绝；
//   - panic：做同样的撤销与标记后原样重抛。否则内层 panic 被外层 recover
//     吞掉后，失败写入仍可能随外层静默提交——与返回 error 的待遇必须一致。
func runTxLifecycleFn(txCtx context.Context, owned bool, fn func(context.Context) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			contextx.ResetAfterCommit(txCtx)
			if !owned {
				contextx.MarkTxRollbackOnly(txCtx)
			}
			panic(r)
		}
	}()
	if err := fn(txCtx); err != nil {
		// 业务 fn 失败：本作用域注册的回调必须全部作废（owner=false 只回退
		// 内层新增部分，owner=true 封存整个事务），否则外层捕获错误继续提交时，
		// 已回滚逻辑的回调仍会被执行。
		contextx.ResetAfterCommit(txCtx)
		if !owned {
			// 内层复用外层事务时失败：默认令整体 rollback-only。外层若捕获错误
			// 继续提交，owner 会在提交点拒绝并回滚数据库写入；需要“局部失败后
			// 继续”的语义时，显式使用保存点作用域，而不是吞掉这里的错误。
			contextx.MarkTxRollbackOnly(txCtx)
		}
		return err
	}
	return nil
}
