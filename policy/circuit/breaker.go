package circuit

import (
	"sync"
	"time"

	"gochen/clock"
	"gochen/errors"
)

// State 熔断器状态。
type State uint8

const (
	// StateClosed 表示闭合状态（正常放行）。
	StateClosed State = iota
	// StateOpen 表示打开状态（拒绝请求）。
	StateOpen
	// StateHalfOpen 表示半开状态（允许一次试探）。
	StateHalfOpen
)

// Config 熔断器配置。
type Config struct {
	MaxFailures  int
	ResetTimeout time.Duration

	// Clock 可选：用于状态机中的时间判断（ResetTimeout 等），便于测试稳定控制时间。
	Clock clock.IClock
}

// Breaker 是一个最小熔断器实现。
type Breaker struct {
	cfg Config

	clk clock.IClock

	failures     int
	lastFailTime time.Time
	state        State
	halfOpenBusy bool

	mu sync.Mutex
}

// New 创建熔断器。
func New(cfg Config) *Breaker {
	if cfg.MaxFailures <= 0 {
		cfg.MaxFailures = 5
	}
	if cfg.ResetTimeout <= 0 {
		cfg.ResetTimeout = 10 * time.Second
	}
	if cfg.Clock == nil {
		cfg.Clock = clock.NewRealClock()
	}
	return &Breaker{
		cfg:   cfg,
		clk:   cfg.Clock,
		state: StateClosed,
	}
}

// Call 通过熔断器执行函数。
//
// 说明：
// - Open 状态且未到 ResetTimeout：直接拒绝；
// - 超过 ResetTimeout：进入 HalfOpen，允许一次试探；
// - 试探成功：回到 Closed；失败：回到 Open。
func (b *Breaker) Call(fn func() error) error {
	if fn == nil {
		return errors.NewCode(errors.InvalidInput, "circuit breaker fn is nil")
	}
	if b == nil {
		return fn()
	}

	now := b.clk.Now()

	b.mu.Lock()
	switch b.state {
	case StateOpen:
		if now.Sub(b.lastFailTime) <= b.cfg.ResetTimeout {
			b.mu.Unlock()
			return errors.NewCode(errors.ServiceUnavailable, "service temporarily unavailable")
		}
		// 进入 HalfOpen：允许一次试探（并发下只允许一个 in-flight）。
		b.state = StateHalfOpen
		b.failures = 0
		b.halfOpenBusy = true
	case StateHalfOpen:
		// HalfOpen 期间仅允许一个试探，其余请求拒绝。
		if b.halfOpenBusy {
			b.mu.Unlock()
			return errors.NewCode(errors.ServiceUnavailable, "service temporarily unavailable")
		}
		b.halfOpenBusy = true
	case StateClosed:
		// allow
	default:
		// 未知状态：fail-close（更安全）
		b.mu.Unlock()
		return errors.NewCode(errors.ServiceUnavailable, "service temporarily unavailable")
	}
	b.mu.Unlock()

	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				// 确保 HalfOpen 的 busy 标记不会因 panic 永久卡死；并将状态推进到 Open。
				b.mu.Lock()
				if b.state == StateHalfOpen {
					b.halfOpenBusy = false
					b.failures = b.cfg.MaxFailures
					b.lastFailTime = b.clk.Now()
					b.state = StateOpen
				}
				b.mu.Unlock()
				panic(r)
			}
		}()
		err = fn()
	}()

	b.mu.Lock()
	defer b.mu.Unlock()

	// HalfOpen：试探结束后必须释放 halfOpenBusy，并根据结果转移状态。
	if b.state == StateHalfOpen {
		b.halfOpenBusy = false
		if err != nil {
			// 试探失败：立即回到 Open 并重置计时（避免在 HalfOpen 内被正常 failures 门限吞掉）。
			b.failures = b.cfg.MaxFailures
			b.lastFailTime = b.clk.Now()
			b.state = StateOpen
			return err
		}

		// 试探成功：回到 Closed。
		b.failures = 0
		b.state = StateClosed
		return nil
	}

	if err != nil {
		b.failures++
		b.lastFailTime = b.clk.Now()
		if b.failures >= b.cfg.MaxFailures {
			b.state = StateOpen
		}
		return err
	}

	b.failures = 0
	b.state = StateClosed
	return nil
}
