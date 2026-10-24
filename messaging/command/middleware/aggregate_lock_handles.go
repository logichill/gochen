package middleware

import (
	"context"
	"sync/atomic"
	"time"
)

// lockEntry 锁条目，包含锁信号量和最后使用时间。
type lockEntry struct {
	sem      chan struct{}
	lastUsed time.Time
	// active 表示当前正在使用或等待该锁的 goroutine 数。
	// 只有 active==0 的锁才允许被清理/淘汰，避免出现同一聚合对应多把锁的并发破坏。
	active int
}

type typeLockEntry struct {
	sem      chan struct{}
	lastUsed time.Time
	active   int
}

// aggregateLockHandle 聚合锁句柄。
//
// 用于在不暴露内部计数的前提下，安全地在 Unlock 时回收 active 计数。
// released 保证 Unlock 严格幂等：重复调用（如重复 defer）不会向二值信号量
// 超额归还令牌，也不会重复扣减 active 引用计数，避免互斥失效。
type aggregateLockHandle struct {
	aggregateID string
	entry       *lockEntry
	middleware  *AggregateLockMiddleware
	released    atomic.Bool
}

// Lock 提供带 Context 超时感知的加锁能力。
func (h *aggregateLockHandle) Lock(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-h.entry.sem:
		if err := ctx.Err(); err != nil {
			h.entry.sem <- struct{}{}
			h.middleware.releaseAggregateLock(h.aggregateID)
			return err
		}
		return nil
	case <-ctx.Done():
		h.middleware.releaseAggregateLock(h.aggregateID)
		return ctx.Err()
	}
}

// Unlock 释放锁。重复调用是空操作。
func (h *aggregateLockHandle) Unlock() {
	if !h.released.CompareAndSwap(false, true) {
		return
	}
	h.entry.sem <- struct{}{}
	h.middleware.releaseAggregateLock(h.aggregateID)
}

type typeLockHandle struct {
	aggregateType string
	entry         *typeLockEntry
	middleware    *AggregateLockMiddleware
	released      atomic.Bool
}

func (h *typeLockHandle) Lock(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-h.entry.sem:
		if err := ctx.Err(); err != nil {
			h.entry.sem <- struct{}{}
			h.middleware.releaseTypeLock(h.aggregateType)
			return err
		}
		return nil
	case <-ctx.Done():
		h.middleware.releaseTypeLock(h.aggregateType)
		return ctx.Err()
	}
}

func (h *typeLockHandle) Unlock() {
	if !h.released.CompareAndSwap(false, true) {
		return
	}
	h.entry.sem <- struct{}{}
	h.middleware.releaseTypeLock(h.aggregateType)
}

// getOrCreateAggregateLockHandle 获取或创建聚合锁句柄。
func (m *AggregateLockMiddleware) getOrCreateAggregateLockHandle(aggregateID string) *aggregateLockHandle {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// 先清理过期锁
	m.cleanupExpiredLocks()

	entry, exists := m.locksByAggregate[aggregateID]
	if !exists {
		// 超过限制则淘汰最旧锁
		if m.maxAggregateLocks > 0 && len(m.locksByAggregate) >= m.maxAggregateLocks {
			m.evictOldestLocks()
		}

		now := m.clock.Now()
		entry = &lockEntry{
			sem:      make(chan struct{}, 1),
			lastUsed: now,
			active:   0,
		}
		entry.sem <- struct{}{}
		m.locksByAggregate[aggregateID] = entry
	}

	// 增加活跃计数并更新时间戳
	entry.active++
	entry.lastUsed = m.clock.Now()

	return &aggregateLockHandle{
		aggregateID: aggregateID,
		entry:       entry,
		middleware:  m,
	}
}

// releaseAggregateLock 释放聚合锁引用计数。
func (m *AggregateLockMiddleware) releaseAggregateLock(aggregateID string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	entry, ok := m.locksByAggregate[aggregateID]
	if !ok || entry == nil {
		return
	}
	if entry.active > 0 {
		entry.active--
	}
	entry.lastUsed = m.clock.Now()
}

// getOrCreateTypeLockHandle 获取或创建类型锁句柄。
func (m *AggregateLockMiddleware) getOrCreateTypeLockHandle(aggregateType string) *typeLockHandle {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.cleanupExpiredLocks()

	entry, ok := m.locksByType[aggregateType]
	if !ok {
		entry = &typeLockEntry{
			sem:      make(chan struct{}, 1),
			lastUsed: m.clock.Now(),
		}
		entry.sem <- struct{}{}
		m.locksByType[aggregateType] = entry
	}
	entry.active++
	entry.lastUsed = m.clock.Now()

	return &typeLockHandle{
		aggregateType: aggregateType,
		entry:         entry,
		middleware:    m,
	}
}

func (m *AggregateLockMiddleware) releaseTypeLock(aggregateType string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	entry, ok := m.locksByType[aggregateType]
	if !ok || entry == nil {
		return
	}
	if entry.active > 0 {
		entry.active--
	}
	entry.lastUsed = m.clock.Now()
}
