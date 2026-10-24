// Package writeflow 提供应用层写流程的统一装配与执行引擎。
package writeflow

import (
	"context"
	"fmt"

	"gochen/contextx"
	"gochen/errors"
)

// ITxRunner 提供最小事务执行能力。
type ITxRunner interface {
	WithinTx(ctx context.Context, fn func(txCtx context.Context) error) error
}

// ValidateBatchSize 校验批量操作数量不超过配置上限。
func ValidateBatchSize(size int, max int) error {
	if size > max {
		return errors.NewCode(errors.Validation, fmt.Sprintf("batch size exceeds maximum limit of %d", max))
	}
	return nil
}

// Flow 描述一次写流程，固定按 before -> validate -> action -> after -> post-commit 执行。
//
// 约定：
//   - Before/Validate/Action/After 任一失败立即中断，有事务时随之回滚；
//   - PostCommit 在事务真正提交后执行，失败不会回滚已提交写入；
//   - 配置了 PostCommit 但未绑定事务执行器时，入口即返回 Unsupported；
//   - 构造完成后 Flow 只读，可并发复用。
type Flow[P any] struct {
	tx ITxRunner

	action     func(context.Context, P) error
	befores    []func(context.Context, P) error
	validates  []func(P) error
	afters     []func(context.Context, P) error
	postCommit func(context.Context, P) error

	// outsideTx 将 Before/Validate 移到事务外预检（批量场景）。
	outsideTx bool
}

// New 创建绑定事务执行器的写流程；tx 为 nil 表示仓储不支持事务。
func New[P any](tx ITxRunner) *Flow[P] {
	return &Flow[P]{tx: tx}
}

// Action 绑定核心写入动作；未配置时 Run 返回错误。
func (f *Flow[P]) Action(action func(context.Context, P) error) *Flow[P] {
	f.action = action
	return f
}

// Before 追加写入前步骤；nil 视为未配置。
func (f *Flow[P]) Before(fn func(context.Context, P) error) *Flow[P] {
	if fn != nil {
		f.befores = append(f.befores, fn)
	}
	return f
}

// Validate 追加校验步骤；nil 视为未配置。
func (f *Flow[P]) Validate(fn func(P) error) *Flow[P] {
	if fn != nil {
		f.validates = append(f.validates, fn)
	}
	return f
}

// After 追加核心写入后（事务提交前）步骤；nil 视为未配置。
func (f *Flow[P]) After(fn func(context.Context, P) error) *Flow[P] {
	if fn != nil {
		f.afters = append(f.afters, fn)
	}
	return f
}

// PostCommit 配置事务提交后回调；nil 视为未配置。
func (f *Flow[P]) PostCommit(fn func(context.Context, P) error) *Flow[P] {
	if fn != nil {
		f.postCommit = fn
	}
	return f
}

// OutsideTx 将 Before/Validate 移到事务外预检。
func (f *Flow[P]) OutsideTx() *Flow[P] {
	f.outsideTx = true
	return f
}

// Run 执行写流程。
func (f *Flow[P]) Run(ctx context.Context, payload P) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if f.action == nil {
		return errors.NewCode(errors.InvalidInput, "action step is nil")
	}
	if f.postCommit != nil && f.tx == nil {
		return errors.NewCode(errors.Unsupported, "post-commit hook requires repository to implement ITransactional")
	}

	preflight := func(ctx context.Context) error {
		for _, fn := range f.befores {
			if err := fn(ctx, payload); err != nil {
				return err
			}
		}
		for _, fn := range f.validates {
			if err := fn(payload); err != nil {
				return err
			}
		}
		return nil
	}
	write := func(ctx context.Context) error {
		if err := f.action(ctx, payload); err != nil {
			return err
		}
		for _, fn := range f.afters {
			if err := fn(ctx, payload); err != nil {
				return err
			}
		}
		return nil
	}

	if f.outsideTx {
		if err := preflight(ctx); err != nil {
			return err
		}
	}
	if f.tx == nil {
		if !f.outsideTx {
			if err := preflight(ctx); err != nil {
				return err
			}
		}
		return write(ctx)
	}

	postCommit := f.postCommit
	return f.tx.WithinTx(ctx, func(txCtx context.Context) error {
		if !f.outsideTx {
			if err := preflight(txCtx); err != nil {
				return err
			}
		}
		if err := write(txCtx); err != nil {
			return err
		}
		if postCommit == nil {
			return nil
		}
		// 提交后回调用调用方 ctx 执行，不触碰事务资源。
		return contextx.AppendAfterCommit(txCtx, func(context.Context) error {
			return postCommit(ctx, payload)
		})
	})
}
