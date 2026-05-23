package orm

import (
	"context"
	"gochen/contextx"
	"gochen/errors"
)

type txSessionState struct {
	session IOrmSession
	owned   bool
}

type txSessionStateKey struct{}

type afterCommitDispatcherProvider interface {
	AfterCommitDispatcher() contextx.IAfterCommitDispatcher
}

func AfterCommitDispatcherFromSession(session IOrmSession) (contextx.IAfterCommitDispatcher, bool) {
	if session == nil {
		return nil, false
	}
	provider, ok := session.(afterCommitDispatcherProvider)
	if !ok || provider == nil {
		return nil, false
	}
	dispatcher := provider.AfterCommitDispatcher()
	if dispatcher == nil {
		return nil, false
	}
	return dispatcher, true
}

// WithTxSession 将事务会话写入 ctx，供仓储/审计存储在同一事务中工作。
//
// 说明：
// - owned 表示“当前调用方是否拥有该事务”的提交/回滚职责：
// - - owned=true：应由当前作用域负责 Commit/Rollback
// - - owned=false：表示复用外层事务（当前作用域不应再次提交/回滚）
func WithTxSession(ctx context.Context, session IOrmSession, owned bool) (context.Context, error) {
	if ctx == nil {
		return nil, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if session == nil {
		return nil, errors.NewCode(errors.InvalidInput, "session is nil")
	}
	ctx = context.WithValue(ctx, txSessionStateKey{}, txSessionState{session: session, owned: owned})
	if dispatcher, ok := AfterCommitDispatcherFromSession(session); ok {
		var err error
		ctx, err = contextx.WithAfterCommitDispatcher(ctx, dispatcher)
		if err != nil {
			return nil, err
		}
	}
	return ctx, nil
}

// TxSessionFromContext 从 ctx 取出事务会话及 owned 标记。
// 返回：
// - owned：是否满足条件。
// - ok：是否满足条件。
func TxSessionFromContext(ctx context.Context) (session IOrmSession, owned bool, ok bool) {
	if ctx == nil {
		return nil, false, false
	}
	v, ok := ctx.Value(txSessionStateKey{}).(txSessionState)
	if !ok || v.session == nil {
		return nil, false, false
	}
	return v.session, v.owned, true
}

// SessionFromContext 从 ctx 取出事务会话（忽略 owned）。
func SessionFromContext(ctx context.Context) (IOrmSession, bool) {
	session, _, ok := TxSessionFromContext(ctx)
	return session, ok
}
