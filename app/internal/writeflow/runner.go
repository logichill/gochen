package writeflow

import (
	"context"

	"gochen/contextx"
	"gochen/errors"
)

// ITxRunner 提供最小事务执行能力。
type ITxRunner interface {
	WithinTx(ctx context.Context, fn func(txCtx context.Context) error) error
}

// Step 表示写流程中的一个阶段。
type Step func(ctx context.Context) error

// PostCommit 表示事务提交后执行的回调。
type PostCommit func(ctx context.Context) error

// Plan 描述一次写流程。
type Plan struct {
	Before   Step
	Validate Step
	Write    Step
	After    Step

	PostCommits []PostCommit

	// CallbackContext 用于 post-commit 回调；为空时回退到 Run 的 ctx。
	CallbackContext context.Context

	// BeforeValidateOutsideTx 用于保持 batch 场景现有的预检查语义。
	BeforeValidateOutsideTx bool
}

// Run 按 before -> validate -> write -> after -> register post-commit 执行写流程。
func Run(ctx context.Context, tx ITxRunner, plan Plan) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if plan.Write == nil {
		return errors.NewCode(errors.InvalidInput, "write step is nil")
	}

	callbackCtx := plan.CallbackContext
	if callbackCtx == nil {
		callbackCtx = ctx
	}

	if tx == nil && hasPostCommitCallbacks(plan.PostCommits) {
		return errors.NewCode(errors.Unsupported, "post-commit hook requires repository to implement ITransactional")
	}

	if plan.BeforeValidateOutsideTx {
		if err := runBeforeValidate(ctx, plan); err != nil {
			return err
		}
	}

	if tx == nil {
		if !plan.BeforeValidateOutsideTx {
			if err := runBeforeValidate(ctx, plan); err != nil {
				return err
			}
		}
		return runWriteAndAfter(ctx, plan)
	}

	return tx.WithinTx(ctx, func(txCtx context.Context) error {
		if !plan.BeforeValidateOutsideTx {
			if err := runBeforeValidate(txCtx, plan); err != nil {
				return err
			}
		}
		if err := runWriteAndAfter(txCtx, plan); err != nil {
			return err
		}
		return appendPostCommitCallbacks(txCtx, callbackCtx, plan.PostCommits)
	})
}

func runBeforeValidate(ctx context.Context, plan Plan) error {
	if plan.Before != nil {
		if err := plan.Before(ctx); err != nil {
			return err
		}
	}
	if plan.Validate != nil {
		if err := plan.Validate(ctx); err != nil {
			return err
		}
	}
	return nil
}

func runWriteAndAfter(ctx context.Context, plan Plan) error {
	if err := plan.Write(ctx); err != nil {
		return err
	}
	if plan.After != nil {
		if err := plan.After(ctx); err != nil {
			return err
		}
	}
	return nil
}

func hasPostCommitCallbacks(callbacks []PostCommit) bool {
	for _, callback := range callbacks {
		if callback != nil {
			return true
		}
	}
	return false
}

func appendPostCommitCallbacks(txCtx, callbackCtx context.Context, callbacks []PostCommit) error {
	for _, callback := range callbacks {
		if callback == nil {
			continue
		}
		cb := callback
		if err := contextx.AppendAfterCommit(txCtx, func(context.Context) error {
			return cb(callbackCtx)
		}); err != nil {
			return err
		}
	}
	return nil
}
