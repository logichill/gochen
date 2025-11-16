package retry

import (
	"context"
	"time"
)

// Operation 可重试的操作函数类型
type Operation func(ctx context.Context) error

// Config 重试配置
type Config struct {
	MaxAttempts   int           // 最大尝试次数（包括首次）
	InitialDelay  time.Duration // 初始退避延迟
	BackoffFactor float64       // 退避倍数（指数退避）
	MaxDelay      time.Duration // 最大延迟
}

// DefaultConfig 返回默认配置
//
// 默认值：
//   - MaxAttempts: 2（1次初始 + 1次重试）
//   - InitialDelay: 2ms
//   - BackoffFactor: 2.0（指数退避）
//   - MaxDelay: 1s
func DefaultConfig() Config {
	return Config{
		MaxAttempts:   2,
		InitialDelay:  2 * time.Millisecond,
		BackoffFactor: 2.0,
		MaxDelay:      1 * time.Second,
	}
}

// Do 执行带重试的操作
//
// 参数：
//   - ctx: 上下文（支持取消）
//   - op: 要执行的操作
//   - cfg: 重试配置
//
// 返回：
//   - 最后一次执行的错误（如果所有尝试都失败）
//   - nil（如果任意一次尝试成功）
//
// 使用示例：
//
//	err := retry.Do(ctx, func(ctx context.Context) error {
//	    return someOperation()
//	}, retry.DefaultConfig())
func Do(ctx context.Context, op Operation, cfg Config) error {
	var lastErr error

	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		// 检查上下文是否已取消
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// 执行操作
		err := op(ctx)
		if err == nil {
			return nil // 成功
		}

		lastErr = err

		// 最后一次尝试不需要等待
		if attempt < cfg.MaxAttempts {
			// 计算退避延迟（指数退避）
			delay := time.Duration(float64(cfg.InitialDelay) *
				pow(cfg.BackoffFactor, float64(attempt-1)))

			// 限制最大延迟
			if delay > cfg.MaxDelay {
				delay = cfg.MaxDelay
			}

			// 等待退避延迟（支持上下文取消）
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	return lastErr
}

// DoWithInfo 执行带重试的操作，并提供尝试次数信息
//
// 与 Do 的区别是 Operation 函数会接收当前尝试次数
type OperationWithInfo func(ctx context.Context, attempt int) error

// DoWithInfo 执行带重试的操作，每次尝试都会传入当前尝试次数
func DoWithInfo(ctx context.Context, op OperationWithInfo, cfg Config) error {
	var lastErr error

	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		// 检查上下文是否已取消
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// 执行操作（传入尝试次数）
		err := op(ctx, attempt)
		if err == nil {
			return nil // 成功
		}

		lastErr = err

		// 最后一次尝试不需要等待
		if attempt < cfg.MaxAttempts {
			// 计算退避延迟（指数退避）
			delay := time.Duration(float64(cfg.InitialDelay) *
				pow(cfg.BackoffFactor, float64(attempt-1)))

			// 限制最大延迟
			if delay > cfg.MaxDelay {
				delay = cfg.MaxDelay
			}

			// 等待退避延迟（支持上下文取消）
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	return lastErr
}

// pow 简单的幂运算实现（避免引入 math 包）
func pow(base, exp float64) float64 {
	if exp == 0 {
		return 1
	}
	result := base
	for i := 1; i < int(exp); i++ {
		result *= base
	}
	return result
}
