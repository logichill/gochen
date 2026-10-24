package clock

import (
	"sort"
	"sync"
	"time"
)

// ManualClock 是一个可控的确定性时钟实现，主要用于测试（test-only）。
//
// 说明：
// - 调用方通过 Advance() 推进“当前时间”；
// - Timer/Ticker 在以下场景触发：
//   - Advance() 将 now 推进到/超过其计划触发时间；或
//   - Timer.Reset() 被设置到一个 <= Now() 的 deadline（立即触发）。
//
// 投递语义（Delivery semantics）：
// - Timer：每次计划最多投递 1 个值；
// - Ticker：为保证测试稳定性，会使用缓冲并尽量投递每个 tick；当缓冲满时会丢弃 tick（避免阻塞）。
//
// 注意：该类型为测试辅助工具；生产环境建议使用 RealClock，并通过 IClock 接口注入时间来源以保持可替换性。
type ManualClock struct {
	mu sync.Mutex

	now    time.Time
	nextID int64

	timers  map[int64]*manualTimer
	tickers map[int64]*manualTicker
}

// NewManualClock 创建Manual时钟。
func NewManualClock(start time.Time) *ManualClock {
	if start.IsZero() {
		start = time.Unix(0, 0).UTC()
	}
	return &ManualClock{
		now:     start,
		timers:  make(map[int64]*manualTimer),
		tickers: make(map[int64]*manualTicker),
	}
}

func (c *ManualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// NewTimer 创建一个 timer。
func (c *ManualClock) NewTimer(d time.Duration) ITimer {
	c.mu.Lock()
	defer c.mu.Unlock()

	id := c.allocIDLocked()
	mt := &manualTimer{
		clock:    c,
		id:       id,
		ch:       make(chan time.Time, 1),
		active:   true,
		deadline: c.now.Add(d),
	}
	c.timers[id] = mt

	// 对齐 time.NewTimer 语义：当 d <= 0 时立即触发。
	if d <= 0 {
		mt.fireLocked(c.now)
	}
	return mt
}

// NewTicker 创建一个 ticker。
func (c *ManualClock) NewTicker(d time.Duration) (ITicker, error) {
	if d <= 0 {
		return nil, errorInvalidTickerInterval(d)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	id := c.allocIDLocked()
	mt := &manualTicker{
		clock: c,
		id:    id,
		// 缓冲 tick 事件，避免测试用例在“先 Advance 后 drain”时出现非确定性。
		// 缓冲区刻意设置较大，以减少常见测试场景下的 tick 丢失概率。
		ch:       make(chan time.Time, 1024),
		active:   true,
		interval: d,
		next:     c.now.Add(d),
	}
	c.tickers[id] = mt
	return mt, nil
}

// Advance 将当前时间推进 d，并触发所有到期的 timer/ticker。
//
// 说明：
// - 当 d <= 0 时视为 no-op（保持测试行为可预测）。
func (c *ManualClock) Advance(d time.Duration) {
	if d <= 0 {
		return
	}

	c.mu.Lock()
	target := c.now.Add(d)
	c.now = target

	// 触发所有到期（<= now）的 timer。
	// 收集所有到期的 timer，按 deadline 升序排序（若 deadline 相同按 id 升序），保证触发顺序确定。
	var dueTimers []*manualTimer
	for _, t := range c.timers {
		if t != nil && t.active && !t.deadline.After(c.now) {
			dueTimers = append(dueTimers, t)
		}
	}
	sort.Slice(dueTimers, func(i, j int) bool {
		if dueTimers[i].deadline.Equal(dueTimers[j].deadline) {
			return dueTimers[i].id < dueTimers[j].id
		}
		return dueTimers[i].deadline.Before(dueTimers[j].deadline)
	})
	for _, t := range dueTimers {
		t.fireLocked(t.deadline)
	}

	// 触发所有到期（<= now）的 ticker；当接收方较慢导致 channel 满时会丢弃 tick。
	for _, id := range sortedIDs(c.tickers) {
		tk := c.tickers[id]
		if tk == nil || !tk.active {
			continue
		}
		for !tk.next.After(c.now) {
			tk.trySendLocked(tk.next)
			tk.next = tk.next.Add(tk.interval)
		}
	}
	c.mu.Unlock()
}

// sortedIDs 返回 map 键的升序切片，保证定时器触发顺序确定。
func sortedIDs[V any](m map[int64]V) []int64 {
	ids := make([]int64, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

// allocIDLocked 分配ID锁内。
func (c *ManualClock) allocIDLocked() int64 {
	c.nextID++
	return c.nextID
}
