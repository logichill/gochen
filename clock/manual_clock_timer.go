package clock

import (
	"time"
)

type manualTimer struct {
	clock *ManualClock
	id    int64

	ch chan time.Time

	active   bool
	deadline time.Time
}

// C 返回 timer 的触发 channel。
func (t *manualTimer) C() <-chan time.Time { return t.ch }

// Stop 停止manual定时器。
func (t *manualTimer) Stop() bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	return t.stopLocked()
}

func (t *manualTimer) stopLocked() bool {
	wasActive := t.active
	t.active = false
	delete(t.clock.timers, t.id)
	// 对齐 time.Timer：Stop/Reset 返回后不得收到此前未消费的 tick。
	select {
	case <-t.ch:
		wasActive = true
	default:
	}
	return wasActive
}

// Reset 重置状态。
func (t *manualTimer) Reset(d time.Duration) bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()

	wasActive := t.stopLocked()
	t.active = true
	t.deadline = t.clock.now.Add(d)
	t.clock.timers[t.id] = t

	// 若新的 deadline 已经 <= Now()，则立即触发。
	if !t.deadline.After(t.clock.now) {
		t.fireLocked(t.clock.now)
	}
	return wasActive
}

// fireLocked 触发锁内。
func (t *manualTimer) fireLocked(ts time.Time) {
	if !t.active {
		return
	}
	t.active = false

	delete(t.clock.timers, t.id)
	// Best-effort 投递：不阻塞调用方。
	select {
	case t.ch <- ts:
	default:
	}
}

type manualTicker struct {
	clock *ManualClock
	id    int64

	ch chan time.Time

	active   bool
	interval time.Duration
	next     time.Time
}

// C 返回 ticker 的 tick channel。
func (t *manualTicker) C() <-chan time.Time { return t.ch }

// Stop 停止 ticker。
func (t *manualTicker) Stop() {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	t.active = false
	delete(t.clock.tickers, t.id)
}

func (t *manualTicker) trySendLocked(ts time.Time) {
	select {
	case t.ch <- ts:
	default:
	}
}
