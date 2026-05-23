package contextx

import (
	stdctx "context"

	"gochen/errors"
)

// TxScope 显式承载当前事务上下文及 owner 元数据。
type TxScope struct {
	ctx   stdctx.Context
	owned bool
}

// NewTxScope 基于 ctx 和 owner 标记创建事务作用域，并写入 lifecycle 元数据。
func NewTxScope(ctx stdctx.Context, owned bool) (TxScope, error) {
	if ctx == nil {
		return TxScope{}, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	txCtx, err := WithTxLifecycle(ctx, owned)
	if err != nil {
		return TxScope{}, err
	}
	return TxScope{ctx: txCtx, owned: owned}, nil
}

// Context 返回绑定事务 lifecycle 的上下文。
func (s TxScope) Context() stdctx.Context { return s.ctx }

// Owned 返回当前作用域是否负责提交/回滚。
func (s TxScope) Owned() bool { return s.owned }
