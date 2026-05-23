package clock

import "time"

// IClock 抽象“当前时间”以及 timer/ticker 的创建能力。
//
// 设计动机（Rationale）：
// - 生产代码应尽量避免在核心 retry/timeout/poll 循环中直接依赖 time.Now()/time.NewTimer() 等全局函数；
// - 通过注入 IClock 可以提升测试确定性（determinism），并降低时间相关逻辑的偶发失败。
type IClock interface {
	Now() time.Time
	NewTimer(d time.Duration) ITimer
	// NewTicker 创建 ticker；当 d <= 0 时返回 InvalidInput，而不是 panic。
	NewTicker(d time.Duration) (ITicker, error)
}

// ITimer 是 retry/timeout 等核心循环所需的最小 timer 抽象。
//
// 说明：
// - 该接口刻意对齐 time.Timer 的核心行为（C/Stop/Reset），但通过方法暴露以便注入与测试。
type ITimer interface {
	C() <-chan time.Time
	Stop() bool
	Reset(d time.Duration) bool
}

// ITicker 是 polling 等循环所需的最小 ticker 抽象。
type ITicker interface {
	C() <-chan time.Time
	Stop()
}

// RealClock 是基于标准库 time 包的生产时钟实现。
type RealClock struct{}

// NewRealClock 创建Real时钟。
func NewRealClock() *RealClock { return &RealClock{} }

// Now 返回当前时间（wall clock）。
func (c *RealClock) Now() time.Time { return time.Now() }

// NewTimer 创建一个 timer。
func (c *RealClock) NewTimer(d time.Duration) ITimer { return &realTimer{t: time.NewTimer(d)} }

// NewTicker 创建一个 ticker。
func (c *RealClock) NewTicker(d time.Duration) (ITicker, error) {
	if d <= 0 {
		return nil, errorInvalidTickerInterval(d)
	}
	return &realTicker{t: time.NewTicker(d)}, nil
}

type realTimer struct{ t *time.Timer }

// C 返回 timer 的触发 channel。
func (t *realTimer) C() <-chan time.Time { return t.t.C }

// Stop 停止real定时器。
func (t *realTimer) Stop() bool { return t.t.Stop() }

// Reset 重置状态。
func (t *realTimer) Reset(d time.Duration) bool { return t.t.Reset(d) }

type realTicker struct{ t *time.Ticker }

// C 返回 ticker 的 tick channel。
func (t *realTicker) C() <-chan time.Time { return t.t.C }

// Stop 停止 ticker。
func (t *realTicker) Stop() { t.t.Stop() }
