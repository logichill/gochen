package middleware

import (
	"sort"
	"time"
)

func (m *AggregateLockMiddleware) GetLockCount() int {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	switch m.lockGranularity {
	case "type":
		return len(m.locksByType)
	default:
		return len(m.locksByAggregate)
	}
}

// Clear 清空所有已缓存的空闲锁映射，主要用于测试重置。
func (m *AggregateLockMiddleware) Clear() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	for id, entry := range m.locksByAggregate {
		if entry == nil || entry.active <= 0 {
			delete(m.locksByAggregate, id)
		}
	}
	for aggregateType, entry := range m.locksByType {
		if entry == nil || entry.active <= 0 {
			delete(m.locksByType, aggregateType)
		}
	}
}

// Prune 清理当前所有空闲（无活跃持有者）的锁映射，等价于 Clear。
// 正在被持有的锁（active > 0）不会被移除，以保证并发安全。
func (m *AggregateLockMiddleware) Prune() {
	m.Clear()
}

// PruneExpired 删除所有空闲超过阈值的锁。
func (m *AggregateLockMiddleware) PruneExpired() int {
	if m.lockIdleTimeout <= 0 {
		return 0
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()

	return m.cleanupExpiredLocks()
}

// cleanupExpiredLocks 删除空闲超时且当前无活跃持有者的锁；调用者需持有写锁。
func (m *AggregateLockMiddleware) cleanupExpiredLocks() int {
	if m.lockIdleTimeout <= 0 {
		return 0
	}

	now := m.clock.Now()
	cutoff := now.Add(-m.lockIdleTimeout)
	pruned := 0

	for id, entry := range m.locksByAggregate {
		if entry.active == 0 && entry.lastUsed.Before(cutoff) {
			delete(m.locksByAggregate, id)
			pruned++
		}
	}
	for aggregateType, entry := range m.locksByType {
		if entry.active == 0 && entry.lastUsed.Before(cutoff) {
			delete(m.locksByType, aggregateType)
			pruned++
		}
	}

	return pruned
}

// evictOldestLocks 淘汰最久未使用的锁，使锁数量回到上限之内。
func (m *AggregateLockMiddleware) evictOldestLocks() {
	if m.maxAggregateLocks <= 0 {
		return
	}

	// 如果未超过限制，无需淘汰
	if len(m.locksByAggregate) < m.maxAggregateLocks {
		return
	}

	type item struct {
		id string
		t  time.Time
	}
	items := make([]item, 0, len(m.locksByAggregate))
	for id, entry := range m.locksByAggregate {
		items = append(items, item{id: id, t: entry.lastUsed})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].t.Before(items[j].t)
	})

	overflow := len(m.locksByAggregate) - m.maxAggregateLocks + 1
	if overflow <= 0 {
		return
	}

	evicted := 0
	for i := 0; i < len(items) && evicted < overflow; i++ {
		id := items[i].id
		entry := m.locksByAggregate[id]
		if entry == nil {
			delete(m.locksByAggregate, id)
			evicted++
			continue
		}
		if entry.active != 0 {
			continue
		}
		delete(m.locksByAggregate, id)
		evicted++
	}
}
