package commandflow

import (
	"context"
	"fmt"
	"time"

	"gochen/errors"
	"gochen/policy/retry"
)

// AttemptFunc 表示一次命令尝试。
type AttemptFunc[T any] func(ctx context.Context, attempt int) (T, error)

// AttemptHook 表示一次尝试完成后的回调。
type AttemptHook[T any] func(ctx context.Context, attempt int, state T, err error)

// FinalizeHook 表示整次命令执行结束后的回调。
type FinalizeHook[T any] func(ctx context.Context, attempts int, state T, err error)

// RetryHook 表示一次可重试失败即将进入退避时的回调。
type RetryHook[T any] func(ctx context.Context, attempt int, state T, err error, delay time.Duration)

// TraceHook 表示对整次命令执行做追踪的回调。
type TraceHook func(ctx context.Context, attempts int, elapsed time.Duration, err error)

// WrapFinalError 用于在 runner 出口统一包装最终错误。
type WrapFinalError func(err error, attempts int) error

// Plan 描述一次命令执行流程。
type Plan[T any] struct {
	Attempt AttemptFunc[T]

	RetryConfig retry.Config

	AfterAttempt   AttemptHook[T]
	AfterFinalize  FinalizeHook[T]
	OnRetry        RetryHook[T]
	WrapFinalError WrapFinalError
	Trace          TraceHook
}

// Result 返回命令执行结果摘要。
type Result[T any] struct {
	State    T
	Attempts int
	Elapsed  time.Duration
}

// Run 执行一次“尝试 -> 重试 -> 最终收口”的命令流程。
func Run[T any](ctx context.Context, plan Plan[T]) (Result[T], error) {
	var result Result[T]
	if ctx == nil {
		return result, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if plan.Attempt == nil {
		return result, errors.NewCode(errors.InvalidInput, "attempt func is nil")
	}

	cfg := plan.RetryConfig
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 1
	}
	if cfg.RetryIf == nil {
		cfg.RetryIf = func(error) bool { return false }
	}
	if plan.OnRetry != nil {
		cfg.OnRetry = func(retryCtx context.Context, attempt int, err error, delay time.Duration) {
			plan.OnRetry(retryCtx, attempt, result.State, err, delay)
		}
	}

	startedAt := time.Now()
	// panic 隔离：命令尝试中的 panic（handler/钩子/内部逻辑）会直接展开跳过
	// 收尾钩子，违背 FinalizeHook “每次 ExecuteCommand 只调用一次”的契约。
	// 此处捕获 panic，在完成 WrapFinalError/AfterFinalize/Trace 度量与审计
	// 收尾后重新抛出原始 panic，保持调用方语义不变。
	var panicked any
	var finalErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = r
			}
		}()
		finalErr = retry.DoWithInfo(ctx, func(attemptCtx context.Context, attempt int) error {
			state, err := plan.Attempt(attemptCtx, attempt)
			result.State = state
			result.Attempts = attempt
			if plan.AfterAttempt != nil {
				plan.AfterAttempt(attemptCtx, attempt, state, err)
			}
			return err
		}, cfg)
	}()

	if panicked != nil {
		// 合成的 panic 错误不经过 WrapFinalError（领域错误包装器不应处理 panic 值）。
		finalErr = errors.NewCode(errors.Internal, "command execution panicked").
			WithContext("panic", fmt.Sprint(panicked))
	} else if finalErr != nil && plan.WrapFinalError != nil {
		finalErr = plan.WrapFinalError(finalErr, result.Attempts)
	}
	result.Elapsed = time.Since(startedAt)

	if plan.AfterFinalize != nil {
		plan.AfterFinalize(ctx, result.Attempts, result.State, finalErr)
	}
	if plan.Trace != nil {
		plan.Trace(ctx, result.Attempts, result.Elapsed, finalErr)
	}
	if panicked != nil {
		panic(panicked)
	}
	return result, finalErr
}
