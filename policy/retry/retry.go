package retry

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/binary"
	"math"
	mrand "math/rand"
	"net"
	"sync"
	"time"

	"gochen/clock"
	"gochen/errors"
)

// Operation 可重试的操作函数类型。
type Operation func(ctx context.Context) error

// IRetryableError 可重试错误接口。
//
// 说明：
// - 用于让业务错误显式表达“是否可重试”，避免在 retry 包中耦合具体错误类型；
// - 与 Config.RetryIf 配合，可实现更精细的重试策略。
type IRetryableError interface {
	IsRetryable() bool
}

// Config 重试配置，用于控制重试策略（最大次数、退避策略、超时等）。
type Config struct {
	MaxAttempts   int           // 最大尝试次数（包括首次）
	InitialDelay  time.Duration // 初始退避延迟
	BackoffFactor float64       // 退避倍数（指数退避）
	MaxDelay      time.Duration // 最大延迟

	// Clock 可选：用于创建 timer，避免生产/核心循环直接依赖 time.NewTimer，
	// 并让测试可通过注入 ManualClock 稳定控制“时间推进”。
	Clock clock.IClock

	// JitterRatio 抖动比例（0~1）。
	//
	// 说明：
	// - 0 表示不启用抖动；
	// - 抖动会在退避延迟上做随机扰动，避免大规模并发重试导致惊群。
	JitterRatio float64

	// RetryIf 自定义“是否继续重试”判断函数。
	//
	// 说明：
	// - nil 表示默认策略：除 context 取消/超时外，其他错误都允许重试；
	// - 若返回 false，将立即停止重试并返回该错误；
	// - 典型用法：只对网络错误、5xx、限流错误等重试。
	RetryIf func(err error) bool
}

// DefaultConfig 返回默认配置。
//
// 说明：
// - 默认值：
// - - MaxAttempts: 2（1次初始 + 1次重试）
// - - InitialDelay: 2ms
// - - BackoffFactor: 2.0（指数退避）
// - - MaxDelay: 1s
func DefaultConfig() Config {
	return Config{
		MaxAttempts:   2,
		InitialDelay:  2 * time.Millisecond,
		BackoffFactor: 2.0,
		MaxDelay:      1 * time.Second,
		JitterRatio:   0,
		Clock:         clock.NewRealClock(),
	}
}

// Do 执行带重试的操作。
func Do(ctx context.Context, op Operation, cfg Config) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if op == nil {
		return errors.NewCode(errors.InvalidInput, "op is nil")
	}
	return do(ctx, cfg, func(ctx context.Context, attempt int) error {
		return op(ctx)
	})
}

// DoWithInfo 执行带重试的操作，并提供尝试次数信息。
//
// 与 Do 的区别是 Operation 函数会接收当前尝试次数。
type OperationWithInfo func(ctx context.Context, attempt int) error

// DoWithInfo 执行带重试的操作，每次尝试都会传入当前尝试次数。
func DoWithInfo(ctx context.Context, op OperationWithInfo, cfg Config) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if op == nil {
		return errors.NewCode(errors.InvalidInput, "op is nil")
	}
	return do(ctx, cfg, op)
}

func pow(base, exp float64) float64 {
	return math.Pow(base, exp)
}

// normalizeConfig 规范化配置。
func normalizeConfig(cfg Config) Config {
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 1
	}
	if cfg.BackoffFactor <= 0 {
		cfg.BackoffFactor = 2.0
	}
	if cfg.InitialDelay < 0 {
		cfg.InitialDelay = 0
	}
	if cfg.MaxDelay <= 0 {
		cfg.MaxDelay = cfg.InitialDelay
	}
	if cfg.JitterRatio < 0 {
		cfg.JitterRatio = 0
	}
	if cfg.JitterRatio > 1 {
		cfg.JitterRatio = 1
	}
	if cfg.Clock == nil {
		cfg.Clock = clock.NewRealClock()
	}
	return cfg
}

var (
	defaultJitterMu  sync.Mutex
	defaultJitterRng = mrand.New(mrand.NewSource(defaultJitterSeed()))
)

func defaultJitterSeed() int64 {
	// Best-effort: crypto/rand first (unpredictable across processes), fallback to time-based seed.
	var b [8]byte
	if _, err := cryptorand.Read(b[:]); err == nil {
		u := binary.LittleEndian.Uint64(b[:])
		// avoid returning 0 seed to reduce chance of identical streams under fallback edge cases
		if u != 0 {
			return int64(u)
		}
	}
	return time.Now().UnixNano()
}

func defaultRandFloat64() float64 {
	defaultJitterMu.Lock()
	v := defaultJitterRng.Float64()
	defaultJitterMu.Unlock()
	return v
}

// jitterFloat64 is a package-level hook mainly for tests.
// Production code should not modify it.
var jitterFloat64 = defaultRandFloat64

func do(ctx context.Context, cfg Config, op OperationWithInfo) error {
	cfg = normalizeConfig(cfg)
	clk := cfg.Clock

	var lastErr error
	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		err := op(ctx, attempt)
		if err == nil {
			return nil
		}
		lastErr = err

		retryAllowed := true
		if cfg.RetryIf != nil {
			retryAllowed = cfg.RetryIf(err)
		} else {
			retryAllowed = IsRetryable(err)
		}
		if !retryAllowed {
			return err
		}

		if attempt < cfg.MaxAttempts {
			delay := computeDelay(cfg, attempt)
			if delay > 0 {
				timer := clk.NewTimer(delay)
				select {
				case <-timer.C():
				case <-ctx.Done():
					if !timer.Stop() {
						select {
						case <-timer.C():
						default:
						}
					}
					return ctx.Err()
				}
			}
		}
	}
	return lastErr
}

func computeDelay(cfg Config, attempt int) time.Duration {
	if attempt <= 0 {
		attempt = 1
	}
	delay := time.Duration(float64(cfg.InitialDelay) * pow(cfg.BackoffFactor, float64(attempt-1)))
	if delay > cfg.MaxDelay {
		delay = cfg.MaxDelay
	}
	if delay <= 0 || cfg.JitterRatio <= 0 {
		return delay
	}

	// jitter range: [delay*(1-J), delay*(1+J)] then clamp back to [0, MaxDelay]
	// (MaxDelay is treated as a strict upper bound).
	jitter := cfg.JitterRatio * (jitterFloat64()*2 - 1) // [-J, +J] if rnd in [0,1)
	jittered := time.Duration(float64(delay) * (1 + jitter))
	if jittered < 0 {
		jittered = 0
	}
	if cfg.MaxDelay > 0 && jittered > cfg.MaxDelay {
		jittered = cfg.MaxDelay
	}
	return jittered
}

// ComputeDelay 计算指定 attempt 的退避延迟（包含 jitter）。
//
// 说明：
// - attempt 为 1-based（与 Do/DoWithInfo 内部一致）；
// - 该函数会执行与 Do/DoWithInfo 相同的配置归一化逻辑。
func ComputeDelay(cfg Config, attempt int) time.Duration {
	cfg = normalizeConfig(cfg)
	return computeDelay(cfg, attempt)
}

// IsRetryable 默认的可重试判断。
//
// 说明：
// - 规则：
// - - context.Canceled / context.DeadlineExceeded：不可重试（直接返回 false）
// - - IRetryableError：遵循其 IsRetryable()
// - - net.Error（timeout）：可重试。
// - - 其他错误：默认可重试（保持 retry 包的“通用”行为）
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var re IRetryableError
	if errors.As(err, &re) {
		return re.IsRetryable()
	}
	var ne net.Error
	if errors.As(err, &ne) {
		return ne.Timeout()
	}
	return true
}
