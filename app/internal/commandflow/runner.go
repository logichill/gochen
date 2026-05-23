package commandflow

import (
	"context"
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

	cfg := normalizeRetryConfig(plan.RetryConfig)
	startedAt := time.Now()
	var finalErr error

	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		state, err := plan.Attempt(ctx, attempt)
		result.State = state
		result.Attempts = attempt
		if plan.AfterAttempt != nil {
			plan.AfterAttempt(ctx, attempt, state, err)
		}
		if err == nil {
			finalErr = nil
			break
		}
		if attempt >= cfg.MaxAttempts || !cfg.RetryIf(err) {
			finalErr = err
			break
		}

		delay := retry.ComputeDelay(cfg, attempt)
		if plan.OnRetry != nil {
			plan.OnRetry(ctx, attempt, state, err, delay)
		}
		if waitErr := waitBackoff(ctx, delay); waitErr != nil {
			finalErr = waitErr
			break
		}
	}

	if finalErr != nil && plan.WrapFinalError != nil {
		finalErr = plan.WrapFinalError(finalErr, result.Attempts)
	}
	result.Elapsed = time.Since(startedAt)

	if plan.AfterFinalize != nil {
		plan.AfterFinalize(ctx, result.Attempts, result.State, finalErr)
	}
	if plan.Trace != nil {
		plan.Trace(ctx, result.Attempts, result.Elapsed, finalErr)
	}
	return result, finalErr
}

func normalizeRetryConfig(cfg retry.Config) retry.Config {
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 1
	}
	if cfg.RetryIf == nil {
		cfg.RetryIf = func(error) bool { return false }
	}
	return cfg
}

func waitBackoff(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
