package circuit

import (
	"context"
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

	// CallTimeout 可选：单次调用的超时预算，仅对 CallContext 生效。
	// 到期后熔断器以专属 cause（errBreakerCallTimeout）取消派生给 fn 的 ctx；
	// 该超时代表下游在熔断器预算内未完成，属于下游过慢信号，计失败并可触发
	// 跳闸。fn 必须尊重传入 ctx 的取消，否则超时无法中断执行。0 表示不启用。
	CallTimeout time.Duration

	// Clock 可选：用于状态机中的时间判断（ResetTimeout 等），便于测试稳定控制时间。
	Clock clock.IClock
}

// errBreakerCallTimeout 是熔断器自身调用超时使用的专属取消 cause。
// CallContext 通过对比派生 ctx 的 cause 与该哨兵，把「熔断器预算超时」与
// 「调用方取消」在 cause 层面区分开：前者是下游过慢信号（计失败），后者不是。
var errBreakerCallTimeout = errors.New("circuit breaker: call timeout")

// errBreakerCallDone 在调用结束后释放派生 ctx，不参与任何失败判定。
var errBreakerCallDone = errors.New("circuit breaker: call finished")

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
//
// 失败计数语义：Call 只有 func() error 签名，没有 ctx 信息，无法区分
// 「调用方取消」与「下游故障」——任何非 nil 错误（含 context.Canceled/
// DeadlineExceeded 形态）一律计失败。需要按 ctx 区分取消来源时使用
// CallContext；需要豁免调用方取消的调用方应迁移到 CallContext。
func (b *Breaker) Call(fn func() error) error {
	if fn == nil {
		return errors.NewCode(errors.InvalidInput, "circuit breaker fn is nil")
	}
	if b == nil {
		return fn()
	}
	return b.doCall(func() (error, bool) {
		return fn(), false
	})
}

// CallContext 在熔断器保护下执行 fn，并向 fn 传入由调用方 ctx 派生的 ctx。
//
// 与 Call 的区别：CallContext 知道调用方 ctx 的生命周期，可以按「取消来源」
// 区分失败计数（否则下游纯超时与调用方取消在错误形态上无法区分——两者都是
// context.DeadlineExceeded/context.Canceled）：
//   - 豁免（不计失败）：fn 返回 ctx 哨兵错误，且派生 ctx 的 cause 表明取消
//     源于调用方 ctx（cause 非空且不是熔断器超时）。调用方主动放弃等待不能
//     证明下游故障。
//   - 计失败（可触发跳闸）：
//   - 熔断器自身 CallTimeout 到期（cause == errBreakerCallTimeout）；
//   - 下游自身超时：调用方 ctx 仍存活（cause 为空）时 fn 返回
//     DeadlineExceeded/Canceled（如 handler 派生 deadline 到期、下游内部
//     自取消）；
//   - 一切非 ctx 形态的错误。
func (b *Breaker) CallContext(ctx context.Context, fn func(context.Context) error) error {
	if fn == nil {
		return errors.NewCode(errors.InvalidInput, "circuit breaker fn is nil")
	}
	if b == nil {
		return fn(ctx)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// WithCancelCause 让熔断器能够回答「这次取消是谁发起的」：
	// 调用方 ctx 取消会携带调用方的 cause 传播进来，熔断器自身超时以
	// errBreakerCallTimeout 取消，二者可对比区分；调用方 ctx 存活时 cause 为空。
	callCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(errBreakerCallDone)
	var timer *time.Timer
	if b.cfg.CallTimeout > 0 {
		timer = time.AfterFunc(b.cfg.CallTimeout, func() { cancel(errBreakerCallTimeout) })
		defer timer.Stop()
	}
	return b.doCall(func() (error, bool) {
		fnErr := fn(callCtx)
		if fnErr == nil || !isContextSentinelError(fnErr) {
			return fnErr, false
		}
		// fn 返回了 ctx 哨兵错误：用派生 ctx 的 cause 判定取消来源。
		switch cause := context.Cause(callCtx); {
		case cause == nil:
			// 调用方 ctx 仍存活、派生 ctx 未被取消：错误必然源自下游自身
			// （如 handler 派生 deadline 到期）→ 计失败。
			return fnErr, false
		case errors.Is(cause, errBreakerCallTimeout):
			// 熔断器自身超时：下游在预算内未完成 → 计失败。
			return fnErr, false
		default:
			// 调用方 ctx 取消传播到派生 ctx → 豁免。
			return fnErr, true
		}
	})
}

// doCall 执行熔断状态机。execute 负责真正调用受保护函数，并返回
// (错误, 是否为可豁免的调用方取消)。
func (b *Breaker) doCall(execute func() (error, bool)) error {
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

	var fnErr error
	var callerCanceled bool
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
		fnErr, callerCanceled = execute()
	}()

	b.mu.Lock()
	defer b.mu.Unlock()

	// 调用方主动取消：取消只代表调用方放弃等待，不能证明下游故障。
	// 仅当错误被确认源于调用方 ctx 取消时豁免（见 CallContext；Call 路径
	// 没有任何 ctx 信息，永不豁免）。计入失败会在客户端高频断连场景下把
	// 熔断器误跳闸，也会让 HalfOpen 试探因调用方超时被永久误判失败。
	if fnErr != nil && callerCanceled {
		if isHalfOpenProbe && b.state == StateHalfOpen {
			b.halfOpenBusy = false
		}
		return fnErr
	}

	// HalfOpen：只有本次调用确认为半开试探时，才允许收口 HalfOpen 状态。
	if isHalfOpenProbe && b.state == StateHalfOpen {
		b.halfOpenBusy = false
		if fnErr != nil {
			// 试探失败：立即回到 Open 并重置计时（避免在 HalfOpen 内被正常 failures 门限吞掉）。
			b.failures = b.cfg.MaxFailures
			b.openLocked(b.clk.Now())
			return fnErr
		}

		// 试探成功：回到 Closed。
		b.failures = 0
		b.resetWindow()
		b.state = StateClosed
		return nil
	}

	if b.generation != startGeneration || b.state != StateClosed {
		return fnErr
	}

	if b.windowMode() {
		b.recordResult(fnErr != nil)
		if b.windowTripped() {
			// 跳闸时以当前时刻为 Open 起点；触发跳闸的可能是补齐窗口的成功调用，
			// 若只在失败分支刷新 lastFailTime，会沿用更早失败的时间戳导致 ResetTimeout 冷却被缩短甚至跳过。
			b.openLocked(b.clk.Now())
			b.resetWindow()
		} else if fnErr != nil {
			b.lastFailTime = b.clk.Now()
		}
		return fnErr
	}

	if fnErr != nil {
		b.failures++
		if b.failures >= b.cfg.MaxFailures {
			b.openLocked(b.clk.Now())
		} else {
			b.lastFailTime = b.clk.Now()
		}
		return fnErr
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

// isContextSentinelError 判断错误链中是否包含标准库 ctx 哨兵
// （context.Canceled / context.DeadlineExceeded）。
//
// 注意：错误形态本身不足以区分「调用方取消」与「下游超时/自取消」——
// 两者共用同一对哨兵。该判定只作为 CallContext 来源分析的入口，
// 最终以免除与否必须结合派生 ctx 的 cause（见 CallContext），不得仅凭
// 错误形态做豁免。
func isContextSentinelError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
