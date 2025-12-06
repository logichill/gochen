// Package cache 提供统一的缓存抽象层
//
// 设计原则：
// 1. 简洁 - 只包含必需的功能
// 2. 类型安全 - 使用泛型提供编译时类型检查
// 3. 容量管理 - 防止 OOM，自动 LRU 驱逐
// 4. 并发安全 - 使用 RWMutex 保护
package cache

import (
	"container/list"
	"fmt"
	"sync"
	"time"
)

// Cache 通用泛型缓存
//
// 核心特性：
// - 泛型支持：Cache[K comparable, V any]
// - LRU 驱逐：超过容量时自动删除最久未使用的条目
// - TTL 过期：基于访问时间的过期策略
// - 并发安全：RWMutex 保护
// - 完整统计：Hits/Misses/Evictions/Size
//
// 使用示例：
//
//	// 创建聚合缓存
//	cache := cache.New[int64, *UserAggregate](cache.Config{
//	    Name:     "user_aggregate",
//	    MaxSize:  1000,
//	    TTL:      5 * time.Minute,
//	})
//
//	// 设置和获取
//	cache.Set(userID, aggregate)
//	if value, found := cache.Get(userID); found {
//	    // 使用缓存的值
//	}
type Cache[K comparable, V any] struct {
	name   string
	config Config

	// 缓存数据
	items map[K]*cacheEntry[K, V]

	// LRU 链表（最近使用的在前）
	lruList *list.List

	// 并发控制
	mu sync.RWMutex

	// 统计信息
	stats CacheStats
}

// cacheEntry 缓存条目
type cacheEntry[K comparable, V any] struct {
	key        K
	value      V
	createdAt  time.Time
	accessedAt time.Time
	lruElement *list.Element // LRU 链表中的位置
}

// Config 缓存配置
type Config struct {
	// Name 缓存名称（用于日志和统计）
	Name string

	// MaxSize 最大缓存条目数，0 表示无限制（不推荐）
	MaxSize int

	// TTL 缓存过期时间，基于访问时间
	// 0 表示永不过期
	TTL time.Duration

	// EnableStats 是否启用统计（默认启用）
	EnableStats bool

	// OnEvict 驱逐回调（可选）
	// 使用 any 以支持不同的键值类型
	OnEvict func(key, value any)
}

// CacheStats 缓存统计信息
type CacheStats struct {
	Hits      int64 // 缓存命中次数
	Misses    int64 // 缓存未命中次数
	Evictions int64 // LRU 驱逐次数
	Expires   int64 // TTL 过期次数
	Size      int   // 当前条目数
}

// New 创建新的缓存实例
func New[K comparable, V any](config Config) *Cache[K, V] {
	if config.Name == "" {
		config.Name = "unnamed"
	}

	return &Cache[K, V]{
		name:    config.Name,
		config:  config,
		items:   make(map[K]*cacheEntry[K, V]),
		lruList: list.New(),
	}
}

// Get 获取缓存值
//
// 返回：
//   - value: 缓存的值
//   - found: 是否找到且未过期
func (c *Cache[K, V]) Get(key K) (value V, found bool) {
	// 这里使用写锁而不是读锁，是因为 Get 需要更新访问时间、LRU 链表位置以及统计信息，
	// 都会修改内部状态；为了保证 LRU 与统计的一致性，选择在单一写锁下完成读取与更新。
	// 若业务有极端读多写少的高并发场景，可在上层增加只读缓存或副本缓存来分担压力。
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, exists := c.items[key]
	if !exists {
		// 缓存未命中
		c.stats.Misses++
		return value, false
	}

	// 检查是否过期
	if c.isExpired(entry) {
		// 已过期，删除
		c.removeEntryUnsafe(entry)
		c.stats.Misses++
		c.stats.Expires++
		return value, false
	}

	// 缓存命中，更新访问时间和 LRU 位置
	entry.accessedAt = time.Now()
	c.lruList.MoveToFront(entry.lruElement)
	c.stats.Hits++

	return entry.value, true
}

// Set 设置缓存值
func (c *Cache[K, V]) Set(key K, value V) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()

	// 检查是否已存在
	if entry, exists := c.items[key]; exists {
		// 更新现有条目
		entry.value = value
		entry.accessedAt = now
		c.lruList.MoveToFront(entry.lruElement)
		return
	}

	// 检查是否需要驱逐
	if c.config.MaxSize > 0 && len(c.items) >= c.config.MaxSize {
		c.evictOldestUnsafe()
	}

	// 创建新条目
	entry := &cacheEntry[K, V]{
		key:        key,
		value:      value,
		createdAt:  now,
		accessedAt: now,
	}

	// 添加到 LRU 链表头部
	entry.lruElement = c.lruList.PushFront(entry)

	// 添加到 map
	c.items[key] = entry
	c.stats.Size = len(c.items)
}

// Delete 删除缓存条目
//
// 返回：是否存在并被删除
func (c *Cache[K, V]) Delete(key K) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, exists := c.items[key]
	if !exists {
		return false
	}

	c.removeEntryUnsafe(entry)
	return true
}

// Clear 清空所有缓存
func (c *Cache[K, V]) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 如果有驱逐回调，先调用
	if c.config.OnEvict != nil {
		for _, entry := range c.items {
			c.config.OnEvict(entry.key, entry.value)
		}
	}

	c.items = make(map[K]*cacheEntry[K, V])
	c.lruList = list.New()
	c.stats.Size = 0
}

// CleanExpired 清理过期条目
//
// 返回：清理的条目数量
func (c *Cache[K, V]) CleanExpired() int {
	if c.config.TTL <= 0 {
		return 0 // 永不过期
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	cleaned := 0
	now := time.Now()

	for _, entry := range c.items {
		if now.Sub(entry.accessedAt) >= c.config.TTL {
			c.removeEntryUnsafe(entry)
			cleaned++
		}
	}

	c.stats.Expires += int64(cleaned)
	c.stats.Size = len(c.items)

	return cleaned
}

// Stats 获取缓存统计信息（副本）
func (c *Cache[K, V]) Stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := c.stats
	stats.Size = len(c.items)
	return stats
}

// Size 获取当前缓存条目数
func (c *Cache[K, V]) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}

// HitRate 获取缓存命中率
func (c *Cache[K, V]) HitRate() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	total := c.stats.Hits + c.stats.Misses
	if total == 0 {
		return 0
	}
	return float64(c.stats.Hits) / float64(total)
}

// isExpired 检查条目是否过期（需要持锁调用）
func (c *Cache[K, V]) isExpired(entry *cacheEntry[K, V]) bool {
	if c.config.TTL <= 0 {
		return false
	}
	return time.Since(entry.accessedAt) >= c.config.TTL
}

// evictOldestUnsafe 驱逐最久未使用的条目（需要持锁调用）
func (c *Cache[K, V]) evictOldestUnsafe() {
	// 从 LRU 链表尾部获取最久未使用的条目
	oldest := c.lruList.Back()
	if oldest == nil {
		return
	}

	entry := oldest.Value.(*cacheEntry[K, V])
	c.removeEntryUnsafe(entry)
	c.stats.Evictions++
}

// removeEntryUnsafe 删除条目（需要持锁调用）
func (c *Cache[K, V]) removeEntryUnsafe(entry *cacheEntry[K, V]) {
	// 调用驱逐回调
	if c.config.OnEvict != nil {
		c.config.OnEvict(entry.key, entry.value)
	}

	// 从 LRU 链表中移除
	if entry.lruElement != nil {
		c.lruList.Remove(entry.lruElement)
	}

	// 从 map 中删除
	delete(c.items, entry.key)
	c.stats.Size = len(c.items)
}

// String 返回缓存信息的字符串表示
func (c *Cache[K, V]) String() string {
	stats := c.Stats()
	return fmt.Sprintf("Cache[%s]: size=%d/%d, hits=%d, misses=%d, hit_rate=%.2f%%, evictions=%d, expires=%d",
		c.name,
		stats.Size,
		c.config.MaxSize,
		stats.Hits,
		stats.Misses,
		c.HitRate()*100,
		stats.Evictions,
		stats.Expires,
	)
}
