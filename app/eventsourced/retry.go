package eventsourced

import (
	"fmt"
	"time"

	"gochen/errors"
	"gochen/policy/retry"
)

// RetryConfig 并发冲突重试配置。
//
// 说明：
//   - 该重试用于事件溯源命令执行的“保存阶段”并发冲突（errors.Concurrency），
//     通过重新加载聚合并重新执行 handler 来完成一次新的尝试；
//   - 该策略要求 handler 可重入：避免在 handler 内部产生不可回滚的外部副作用。
type RetryConfig struct {
	// MaxRetries 最大重试次数（不包含首次尝试）。
	// 0 表示不重试，仅执行一次。
	MaxRetries int

	// InitialBackoff 初始退避时间。
	InitialBackoff time.Duration

	// MaxBackoff 最大退避时间。
	MaxBackoff time.Duration

	// BackoffMultiplier 退避时间乘数（指数退避）。
	BackoffMultiplier float64

	// JitterRatio 抖动比例（0~1）。
	//
	// 说明：
	// - 0 表示不启用抖动；
	// - 抖动会在退避时间上做随机扰动，避免高并发重试导致惊群。
	JitterRatio float64
}

// DefaultRetryConfig 默认重试配置。
func DefaultRetryConfig() *RetryConfig {
	return &RetryConfig{
		MaxRetries:        3,
		InitialBackoff:    10 * time.Millisecond,
		MaxBackoff:        1 * time.Second,
		BackoffMultiplier: 2.0,
		// 默认启用小幅 jitter（最佳实践：避免并发冲突场景下的惊群效应）。
		JitterRatio: 0.2,
	}
}

// IsConcurrencyError 判断是否为并发冲突错误。
//
// 可由调用方自定义，用于识别需要重试的错误类型。
type IsConcurrencyError func(err error) bool

// DefaultIsConcurrencyError 默认并发错误判断（基于错误码）。
func DefaultIsConcurrencyError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, errors.Concurrency)
}

// normalizeRetryConfig 规范化重试配置。
func normalizeRetryConfig(cfg *RetryConfig) *RetryConfig {
	if cfg == nil {
		return nil
	}
	cp := *cfg
	if cp.MaxRetries < 0 {
		cp.MaxRetries = 0
	}
	if cp.InitialBackoff < 0 {
		cp.InitialBackoff = 0
	}
	if cp.MaxBackoff < 0 {
		cp.MaxBackoff = 0
	}
	if cp.MaxBackoff == 0 {
		// MaxBackoff==0 视为“未显式配置最大退避”，默认对齐为 InitialBackoff。
		// 若 InitialBackoff 也为 0，则最终退避始终为 0（立即重试）；这是有效配置，常用于不希望 sleep 的场景。
		cp.MaxBackoff = cp.InitialBackoff
	}
	if cp.BackoffMultiplier <= 0 {
		cp.BackoffMultiplier = 2.0
	}
	if cp.JitterRatio < 0 {
		cp.JitterRatio = 0
	}
	if cp.JitterRatio > 1 {
		cp.JitterRatio = 1
	}
	return &cp
}

func (cfg *RetryConfig) toPolicyConfig(isConcurrencyError IsConcurrencyError) retry.Config {
	if cfg == nil {
		return retry.Config{
			MaxAttempts: 1,
			RetryIf: func(err error) bool {
				_ = err
				return false
			},
		}
	}
	if isConcurrencyError == nil {
		isConcurrencyError = DefaultIsConcurrencyError
	}
	return retry.Config{
		MaxAttempts:   cfg.MaxRetries + 1, // 含首次
		InitialDelay:  cfg.InitialBackoff,
		BackoffFactor: cfg.BackoffMultiplier,
		MaxDelay:      cfg.MaxBackoff,
		JitterRatio:   cfg.JitterRatio,
		RetryIf: func(err error) bool {
			return isConcurrencyError(err)
		},
	}
}

// RetryExhaustedError 重试次数耗尽错误。
type RetryExhaustedError struct {
	Cause      error
	MaxRetries int
}

func (e *RetryExhaustedError) Error() string {
	attempts := e.MaxRetries + 1
	if e.Cause == nil {
		return fmt.Sprintf("retry exhausted after %d attempts", attempts)
	}
	return fmt.Sprintf("retry exhausted after %d attempts: %v", attempts, e.Cause)
}

func (e *RetryExhaustedError) Unwrap() error { return e.Cause }

func (e *RetryExhaustedError) ErrorCode() errors.ErrorCode { return errors.Concurrency }
