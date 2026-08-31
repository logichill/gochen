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

	// WindowSize 启用滑动窗口失败率模式：统计最近 WindowSize 次结果。
	// 为 0 时使用默认的连续失败计数模式（任一成功清零）。
	WindowSize int
	// FailureRateThreshold 为窗口内触发熔断的失败率阈值，取值 (0,1]。
	// 仅在 WindowSize>0 时生效；连续故障但被偶发成功打断的场景由此模式捕获。
	FailureRateThreshold float64
	// MinimumRequests 为窗口内评估失败率所需的最小样本数；为 0 时取 WindowSize。
	MinimumRequests int

	// Clock 可选：用于状态机中的时间判断（ResetTimeout 等），便于测试稳定控制时间。
	Clock clock.IClock
}

// IBreaker 熔断器执行契约接口。
type IBreaker interface {
	Call(fn func() error) error
}

// Breaker 是一个最小熔断器实现。
type Breaker struct {
	cfg Config

	clk clock.IClock

	failures     int
	lastFailTime time.Time
	state        State
	halfOpenBusy bool
	generation   uint64

	samples        []bool
	sampleHead     int
	sampleLen      int
	windowFailures int

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
	b := &Breaker{
		cfg:   cfg,
		clk:   cfg.Clock,
		state: StateClosed,
	}
	if cfg.WindowSize > 0 {
		if cfg.FailureRateThreshold <= 0 || cfg.FailureRateThreshold > 1 {
			cfg.FailureRateThreshold = 0.5
		}
		if cfg.MinimumRequests <= 0 || cfg.MinimumRequests > cfg.WindowSize {
			cfg.MinimumRequests = cfg.WindowSize
		}
		b.cfg = cfg
		b.samples = make([]bool, cfg.WindowSize)
	}
	return b
}

// windowMode 表示是否启用滑动窗口失败率模式。
func (b *Breaker) windowMode() bool {
	return b.cfg.WindowSize > 0
}

// recordResult 将一次结果写入滑动窗口并维护窗口内失败计数。
func (b *Breaker) recordResult(failed bool) {
	if b.sampleLen == b.cfg.WindowSize {
		if b.samples[b.sampleHead] {
			b.windowFailures--
		}
		b.samples[b.sampleHead] = failed
		b.sampleHead = (b.sampleHead + 1) % b.cfg.WindowSize
	} else {
		idx := (b.sampleHead + b.sampleLen) % b.cfg.WindowSize
		b.samples[idx] = failed
		b.sampleLen++
	}
	if failed {
		b.windowFailures++
	}
}

// windowTripped 判断当前窗口失败率是否达到熔断阈值。
func (b *Breaker) windowTripped() bool {
	if b.sampleLen < b.cfg.MinimumRequests {
		return false
	}
	return float64(b.windowFailures) >= b.cfg.FailureRateThreshold*float64(b.sampleLen)
}

// resetWindow 清空滑动窗口统计。
func (b *Breaker) resetWindow() {
	b.sampleHead = 0
	b.sampleLen = 0
	b.windowFailures = 0
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
	startGeneration := b.generation
	isHalfOpenProbe := false
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
		isHalfOpenProbe = true
	case StateHalfOpen:
		// HalfOpen 期间仅允许一个试探，其余请求拒绝。
		if b.halfOpenBusy {
			b.mu.Unlock()
			return errors.NewCode(errors.ServiceUnavailable, "service temporarily unavailable")
		}
		b.halfOpenBusy = true
		isHalfOpenProbe = true
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
				// panic 与普通错误同样记为一次失败；HalfOpen 还要释放 busy 标记，避免永久卡死。
				b.mu.Lock()
				if isHalfOpenProbe && b.state == StateHalfOpen {
					b.halfOpenBusy = false
					b.failures = b.cfg.MaxFailures
					b.openLocked(b.clk.Now())
				} else if b.generation == startGeneration && b.state == StateClosed {
					if b.windowMode() {
						b.recordResult(true)
						if b.windowTripped() {
							b.openLocked(b.clk.Now())
							b.resetWindow()
						}
					} else {
						b.failures++
						if b.failures >= b.cfg.MaxFailures {
							b.openLocked(b.clk.Now())
						} else {
							b.lastFailTime = b.clk.Now()
						}
					}
				}
				b.mu.Unlock()
				panic(r)
			}
		}()
		err = fn()
	}()

	b.mu.Lock()
	defer b.mu.Unlock()

	// HalfOpen：只有本次调用确认为半开试探时，才允许收口 HalfOpen 状态。
	if isHalfOpenProbe && b.state == StateHalfOpen {
		b.halfOpenBusy = false
		if err != nil {
			// 试探失败：立即回到 Open 并重置计时（避免在 HalfOpen 内被正常 failures 门限吞掉）。
			b.failures = b.cfg.MaxFailures
			b.openLocked(b.clk.Now())
			return err
		}

		// 试探成功：回到 Closed。
		b.failures = 0
		b.resetWindow()
		b.state = StateClosed
		return nil
	}

	if b.generation != startGeneration || b.state != StateClosed {
		return err
	}

	if b.windowMode() {
		b.recordResult(err != nil)
		if b.windowTripped() {
			// 跳闸时以当前时刻为 Open 起点；触发跳闸的可能是补齐窗口的成功调用，
			// 若只在失败分支刷新 lastFailTime，会沿用更早失败的时间戳导致 ResetTimeout 冷却被缩短甚至跳过。
			b.openLocked(b.clk.Now())
			b.resetWindow()
		} else if err != nil {
			b.lastFailTime = b.clk.Now()
		}
		return err
	}

	if err != nil {
		b.failures++
		if b.failures >= b.cfg.MaxFailures {
			b.openLocked(b.clk.Now())
		} else {
			b.lastFailTime = b.clk.Now()
		}
		return err
	}

	b.failures = 0
	b.state = StateClosed
	return nil
}

func (b *Breaker) openLocked(now time.Time) {
	if b.state != StateOpen {
		b.generation++
	}
	b.state = StateOpen
	b.lastFailTime = now
}
