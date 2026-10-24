package cache

import (
	"fmt"
)

// CacheStats 缓存统计信息。
type CacheStats struct {
	Hits      int64 // 缓存命中次数
	Misses    int64 // 缓存未命中次数
	Evictions int64 // LRU 驱逐次数
	Expires   int64 // TTL 过期次数
	Size      int   // 当前条目数
}

func (c *Cache[K, V]) Stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := c.stats
	stats.Size = len(c.items)
	return stats
}

func (c *Cache[K, V]) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}

func (c *Cache[K, V]) HitRate() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	total := c.stats.Hits + c.stats.Misses
	if total == 0 {
		return 0
	}
	return float64(c.stats.Hits) / float64(total)
}

func (c *Cache[K, V]) String() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// 在单一锁内计算所有统计信息，避免多次加锁
	stats := c.stats
	stats.Size = len(c.items)

	total := stats.Hits + stats.Misses
	hitRate := 0.0
	if total > 0 {
		hitRate = float64(stats.Hits) / float64(total)
	}

	return fmt.Sprintf("Cache[%s]: size=%d/%d, hits=%d, misses=%d, hit_rate=%.2f%%, evictions=%d, expires=%d",
		c.name,
		stats.Size,
		c.config.MaxSize,
		stats.Hits,
		stats.Misses,
		hitRate*100,
		stats.Evictions,
		stats.Expires,
	)
}
