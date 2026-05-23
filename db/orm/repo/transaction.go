package repo

import (
	"context"
	"database/sql"
	"gochen/contextx"
	"gochen/db/orm"
	"gochen/errors"
)

func ensureAfterCommitPropagation(ctx context.Context, session orm.IOrmSession) error {
	if _, ok := contextx.TxLifecycleFromContext(ctx); ok {
		return nil
	}
	if _, ok := orm.AfterCommitDispatcherFromSession(session); ok {
		return nil
	}
	return errors.NewCode(errors.Unsupported,
		"transaction session cannot propagate post-commit callbacks; use an after-commit-aware session or wrap the outer commit with app/crud.WithTx",
	)
}

func (r *Repo[T, ID]) beginTx(ctx context.Context) (contextx.TxScope, error) {
	if ctx == nil {
		return contextx.TxScope{}, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}

	// 已在事务中：复用外层事务（当前作用域不应再次提交/回滚）。
	if session, _, ok := orm.TxSessionFromContext(ctx); ok {
		if err := ensureAfterCommitPropagation(ctx, session); err != nil {
			return contextx.TxScope{}, err
		}
		txCtx, err := orm.WithTxSession(ctx, session, false)
		if err != nil {
			return contextx.TxScope{}, err
		}
		return contextx.NewTxScope(txCtx, false)
	}
	if session, ok := r.orm.(orm.IOrmSession); ok && session != nil {
		if err := ensureAfterCommitPropagation(ctx, session); err != nil {
			return contextx.TxScope{}, err
		}
		txCtx, err := orm.WithTxSession(ctx, session, false)
		if err != nil {
			return contextx.TxScope{}, err
		}
		return contextx.NewTxScope(txCtx, false)
	}

	session, err := r.orm.BeginTx(ctx, (*sql.TxOptions)(nil))
	if err != nil {
		return contextx.TxScope{}, err
	}
	txCtx, err := orm.WithTxSession(ctx, session, true)
	if err != nil {
		return contextx.TxScope{}, err
	}
	return contextx.NewTxScope(txCtx, true)
}

// BeginTx 开启仓储事务并返回带 lifecycle 元数据的事务作用域。
func (r *Repo[T, ID]) BeginTx(ctx context.Context) (contextx.TxScope, error) {
	return r.beginTx(ctx)
}

// Commit 提交仓储事务作用域。
func (r *Repo[T, ID]) Commit(tx contextx.TxScope) error {
	return r.commitTx(tx)
}

// Rollback 回滚仓储事务作用域。
func (r *Repo[T, ID]) Rollback(tx contextx.TxScope) error {
	return r.rollbackTx(tx)
}

// WithinTx 在同一事务中执行 fn，并在成功时提交、失败时回滚。
func (r *Repo[T, ID]) WithinTx(ctx context.Context, fn func(txCtx context.Context) error) error {
	return contextx.RunTxLifecycle(ctx, r, fn)
}

func (r *Repo[T, ID]) commitTx(tx contextx.TxScope) error {
	session, owned, ok := orm.TxSessionFromContext(tx.Context())
	if !ok {
		return errors.NewCode(errors.InvalidInput, "transaction not started")
	}
	if !owned || !tx.Owned() {
		return nil
	}
	return session.Commit()
}

func (r *Repo[T, ID]) rollbackTx(tx contextx.TxScope) error {
	session, owned, ok := orm.TxSessionFromContext(tx.Context())
	if !ok {
		return errors.NewCode(errors.InvalidInput, "transaction not started")
	}
	if !owned || !tx.Owned() {
		return nil
	}
	return session.Rollback()
}
