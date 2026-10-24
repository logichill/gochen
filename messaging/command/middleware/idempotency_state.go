package middleware

import (
	"context"
	"sort"
	"sync"
	"time"

	"gochen/errors"
)

type idLock struct {
	mu   sync.Mutex
	refs int
}

// isProcessed 判断命令 ID 是否仍在幂等窗口内被视为“已处理”。
func (m *IdempotencyMiddleware) isProcessed(commandID string) bool {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	processedAt, exists := m.processed[commandID]
	if !exists {
		return false
	}

	// 检查是否过期
	if m.clock.Now().Sub(processedAt) > m.ttl {
		return false
	}

	return true
}

// markProcessed 把命令 ID 记录为已处理，并在必要时触发最老记录淘汰。
func (m *IdempotencyMiddleware) markProcessed(commandID string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	now := m.clock.Now()
	_, existed := m.processed[commandID]
	m.processed[commandID] = now
	if existed {
		return
	}

	if m.maxProcessed > 0 && len(m.processed) >= m.evicWatermark {
		// 水位线摊薄：初始水位线为 maxProcessed（越过即触发，行为与逐条淘汰
		// 一致）；触发后一次性排序并淘汰到 maxProcessed，水位线抬升约 10% 容量，
		// 把 O(N log N) 全量排序摊薄到两次触发之间的所有写操作，避免容量打满
		// 后每条命令都在全局写锁下触发排序。两条触发之间临时超出 maxProcessed
		// 至多约 10%，属于幂等窗口的放宽，不影响正确性（isProcessed 仍按 TTL 判定）。
		m.evictOldestTo(m.maxProcessed)
		batch := m.maxProcessed / 10
		if batch < 1 {
			batch = 1
		}
		m.evicWatermark = m.maxProcessed + batch
	}
}

// evictOldestTo 保留最新 target 条记录，非同步淘汰其余最旧记录。
func (m *IdempotencyMiddleware) evictOldestTo(target int) {
	size := len(m.processed)
	if target < 0 {
		target = 0
	}
	if size <= target {
		return
	}
	overflow := size - target

	type kv struct {
		id string
		t  time.Time
	}
	items := make([]kv, 0, size)
	for id, t := range m.processed {
		items = append(items, kv{id: id, t: t})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].t.Before(items[j].t) })
	for i := 0; i < overflow; i++ {
		delete(m.processed, items[i].id)
	}
}

// acquireLock 返回命令 ID 对应的细粒度锁，并增加其引用计数。
func (m *IdempotencyMiddleware) acquireLock(commandID string) *idLock {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if lock, ok := m.locks[commandID]; ok {
		lock.refs++
		return lock
	}

	lock := &idLock{refs: 1}
	m.locks[commandID] = lock
	return lock
}

// releaseLock 释放一次命令锁引用，并在无人使用时回收锁条目。
func (m *IdempotencyMiddleware) releaseLock(commandID string, lock *idLock) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	current, ok := m.locks[commandID]
	if !ok || current != lock {
		return
	}

	lock.refs--
	if lock.refs <= 0 {
		delete(m.locks, commandID)
	}
}

// startCleanupWorker 周期性清理超过 TTL 的命令 ID 记录。
func (m *IdempotencyMiddleware) startCleanupWorker() {
	ticker, err := m.clock.NewTicker(m.cleanupInterval)
	if err != nil {
		return
	}
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C():
			m.cleanup()
		case <-m.stopCleanup:
			return
		}
	}
}

// cleanup 删除所有已经超过 TTL 的命令 ID 记录。
func (m *IdempotencyMiddleware) cleanup() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	now := m.clock.Now()
	for id, processedAt := range m.processed {
		if now.Sub(processedAt) > m.ttl {
			delete(m.processed, id)
		}
	}
}

// Stop 停止后台清理 worker，并保证重复调用也不会 panic。
func (m *IdempotencyMiddleware) Stop(ctx context.Context) error {
	if m == nil {
		return nil
	}
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	m.stopOnce.Do(func() {
		close(m.stopCleanup)
	})
	return nil
}

// Clear 清空所有幂等记录，并保留正在使用的细粒度锁以维持并发串行化。
func (m *IdempotencyMiddleware) Clear() {
	if m == nil {
		return
	}
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.processed = make(map[string]time.Time)
	m.evicWatermark = m.maxProcessed
	for commandID, lock := range m.locks {
		if lock.refs <= 0 {
			delete(m.locks, commandID)
		}
	}
}

func (m *IdempotencyMiddleware) GetProcessedCount() int {
	if m == nil {
		return 0
	}
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return len(m.processed)
}
