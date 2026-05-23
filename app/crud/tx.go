package crud

import (
	"context"
	"gochen/contextx"
	"gochen/errors"
)

// ITransactional 定义应用层使用的事务能力端口。
//
// 说明：
//   - 该接口通过 context 在应用服务与仓储实现之间传递事务边界；
//   - 具体事务实现位于基础设施层，但应用层模板只依赖 “在事务里运行一段逻辑” 这组最小契约；
//   - owner/lifecycle 等细节留在具体实现内部，不再暴露为 app 层接口的一部分。
type ITransactional interface {
	WithinTx(ctx context.Context, fn func(txCtx context.Context) error) error
}

// WithTx 在同一事务中执行 fn，并在成功时提交、失败时回滚。
func WithTx(ctx context.Context, txRepo ITransactional, fn func(txCtx context.Context) error) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if txRepo == nil {
		return errors.NewCode(errors.InvalidInput, "txRepo is nil")
	}
	if fn == nil {
		return errors.NewCode(errors.InvalidInput, "fn is nil")
	}
	return txRepo.WithinTx(ctx, fn)
}

type txLifecycleRunner = contextx.ITxLifecycleRunner

func runWithTxLifecycle(ctx context.Context, txRepo txLifecycleRunner, fn func(txCtx context.Context) error) error {
	return contextx.RunTxLifecycle(ctx, txRepo, fn)
}
