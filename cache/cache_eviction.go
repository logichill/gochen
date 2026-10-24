package cache

// maxOpportunisticSweep 限制 Set 机会式清扫单次扫描的条目数上限（扫描数截断，
// 驱逐数随之受限）：避免在大缓存上把单次 Set 退化为全量扫描（审计 P2#19）。
const maxOpportunisticSweep = 16

// lastOpportunisticSweepScans 返回最近一次 Set 机会式清扫实际扫描的条目数。
// 仅供包内测试断言扫描截断语义（P2#19），不构成公共 API。
func (c *Cache[K, V]) lastOpportunisticSweepScans() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastSweepScanned
}

type eviction[K comparable, V any] struct {
	key   K
	value V
}

// isExpiredLocked 是 isExpired 的持锁版本别名，供 Set 内部调用。
// 调用方需自行持锁。
func (c *Cache[K, V]) isExpiredLocked(entry *cacheEntry[K, V]) bool {
	return c.isExpired(entry)
}

// CleanExpired 主动清理所有已过 TTL 的缓存条目，并返回清理数量。
func (c *Cache[K, V]) CleanExpired() int {
	if c.config.TTL <= 0 {
		return 0 // 永不过期
	}

	c.mu.Lock()

	cleaned := 0
	now := c.clock.Now()
	var evicted []eviction[K, V]

	for _, entry := range c.items {
		if now.Sub(entry.accessedAt) >= c.config.TTL {
			ev := c.removeEntryUnsafe(entry)
			if c.config.OnEvict != nil {
				evicted = append(evicted, ev)
			}
			cleaned++
		}
	}

	if c.statsEnabled {
		c.stats.Expires += int64(cleaned)
	}
	c.stats.Size = len(c.items)
	c.mu.Unlock()
	c.notifyEvictions(evicted)

	return cleaned
}

// isExpired 判断指定条目是否已经超过 TTL；调用方需自行持锁。
func (c *Cache[K, V]) isExpired(entry *cacheEntry[K, V]) bool {
	if c.config.TTL <= 0 {
		return false
	}
	return c.clock.Now().Sub(entry.accessedAt) >= c.config.TTL
}

// evictOldestUnsafe 删除 LRU 链表尾部的最旧条目；调用方需自行持锁。
func (c *Cache[K, V]) evictOldestUnsafe() (eviction[K, V], bool) {
	// 从 LRU 链表尾部获取最久未使用的条目
	oldest := c.lruList.Back()
	if oldest == nil {
		var zero eviction[K, V]
		return zero, false
	}

	entry := oldest.Value.(*cacheEntry[K, V])
	evicted := c.removeEntryUnsafe(entry)
	if c.statsEnabled {
		c.stats.Evictions++
	}
	return evicted, true
}

// removeEntryUnsafe 从 map 和 LRU 链表中同时移除一个条目；调用方需自行持锁。
func (c *Cache[K, V]) removeEntryUnsafe(entry *cacheEntry[K, V]) eviction[K, V] {
	// 从 LRU 链表中移除
	if entry.lruElement != nil {
		c.lruList.Remove(entry.lruElement)
	}

	// 从 map 中删除
	delete(c.items, entry.key)
	c.stats.Size = len(c.items)
	return eviction[K, V]{key: entry.key, value: entry.value}
}

func (c *Cache[K, V]) notifyEvictions(evicted []eviction[K, V]) {
	if c.config.OnEvict == nil {
		return
	}
	// 调用方必须先释放 c.mu，再触发 OnEvict，避免回调反向访问缓存时死锁。
	// 批量驱逐时即便某个回调 panic，也会继续触发剩余回调，最终重新抛出首个 panic。
	var firstPanic any
	havePanic := false
	for _, ev := range evicted {
		func() {
			defer func() {
				if panicValue := recover(); panicValue != nil && !havePanic {
					firstPanic = panicValue
					havePanic = true
				}
			}()
			c.config.OnEvict(ev.key, ev.value)
		}()
	}
	if havePanic {
		panic(firstPanic)
	}
}
