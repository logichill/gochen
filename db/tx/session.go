package tx

import (
	"context"

	"gochen/contextx"
	"gochen/db/orm"
	"gochen/errors"
)

// CommitOwnedSession 在 owner 提交点统一执行提交前检查。
//
// 任何直接持有 IOrmSession 的事务入口（仓储、投影 runner、应用服务）在物理
// 提交处必须走同一逻辑，rollback-only 语义才不会因入口不同而漏网：
//   - 事务被某个内层失败作用域标记 rollback-only：拒绝提交，回滚数据库写入
//     并清空全部提交后回调；
//   - 正常提交：成功后封存回调批次。
func CommitOwnedSession(ctx context.Context, session orm.IOrmSession) error {
	if session == nil {
		return errors.NewCode(errors.InvalidInput, "session is nil")
	}
	if contextx.TxRollbackOnly(ctx) {
		contextx.ResetAfterCommit(ctx)
		if rbErr := session.Rollback(); rbErr != nil {
			return errors.Join(
				errors.NewCode(errors.FailedPrecondition, "transaction was marked rollback-only by a failed nested scope"),
				errors.Wrap(rbErr, errors.Database, "failed to roll back rollback-only transaction"),
			)
		}
		return errors.NewCode(errors.FailedPrecondition,
			"transaction was marked rollback-only by a failed nested scope; use WithinSavepoint for partial failures")
	}
	if err := session.Commit(); err != nil {
		return err
	}
	contextx.MarkTxCommitted(ctx)
	return nil
}

// RollbackOwnedSession 在 owner 回滚点统一执行：清空未执行的提交后回调后回滚。
func RollbackOwnedSession(ctx context.Context, session orm.IOrmSession) error {
	if session == nil {
		return errors.NewCode(errors.InvalidInput, "session is nil")
	}
	contextx.ResetAfterCommit(ctx)
	return session.Rollback()
}

// SessionRunner 把已开启的事务会话适配为标准事务生命周期 runner。
//
// 供直接开启会话的事务入口复用 RunTxLifecycle：与 Repo.WithinTx 共享同一套
// 嵌套 rollback-only、panic 失败语义与提交后回调协议，而不是各自手写
// begin/commit（历史上正是这种旁路让失败写入绕过了提交点检查）。
type SessionRunner struct {
	Session orm.IOrmSession
}

// BeginTx 把已开启的会话写入 ctx（owner=true）并建立生命周期元数据。
func (s SessionRunner) BeginTx(ctx context.Context) (TxScope, error) {
	if s.Session == nil {
		return TxScope{}, errors.NewCode(errors.InvalidInput, "session is nil")
	}
	txCtx, err := orm.WithTxSession(ctx, s.Session, true)
	if err != nil {
		return TxScope{}, err
	}
	return NewTxScope(txCtx, true)
}

// Commit 提交会话事务（含 rollback-only 拒绝检查）。
func (s SessionRunner) Commit(scope TxScope) error {
	session, owned, ok := orm.TxSessionFromContext(scope.Context())
	if !ok {
		return errors.NewCode(errors.InvalidInput, "transaction not started")
	}
	if !owned || !scope.Owned() {
		return nil
	}
	return CommitOwnedSession(scope.Context(), session)
}

// Rollback 回滚会话事务。
func (s SessionRunner) Rollback(scope TxScope) error {
	session, owned, ok := orm.TxSessionFromContext(scope.Context())
	if !ok {
		return errors.NewCode(errors.InvalidInput, "transaction not started")
	}
	if !owned || !scope.Owned() {
		return nil
	}
	return RollbackOwnedSession(scope.Context(), session)
}

var _ ITxLifecycleRunner = SessionRunner{}
