// Package cache 提供统一的缓存抽象层。
//
// 设计原则：
// 1. 简洁 - 只包含必需的功能。
// 2. 类型安全 - 使用泛型提供编译时类型检查。
// 3. 容量管理 - 防止 OOM，自动 LRU 驱逐。
// 4. 并发安全 - 使用 RWMutex 保护。
package cache

import (
	"container/list"
	"fmt"
	"sync"
	"time"

	"gochen/clock"
)

// Cache 通用泛型缓存。
//
// 核心特性：
// - 泛型支持：Cache[K comparable, V any]
// - LRU 驱逐：超过容量时自动删除最久未使用的条目。
// - TTL 过期：基于访问时间的过期策略。
// - 并发安全：RWMutex 保护。
// - 完整统计：Hits/Misses/Evictions/Size
//
// 使用示例：
//
//	// 创建聚合缓存。
//	cache := cache.New[int64, *UserAggregate](cache.Config{
//	    Name:     "user_aggregate",
//	    MaxSize:  1000,
//	    TTL:      5 * time.Minute,
//	})
//
//	// 设置和获取。
//	cache.Set(userID, aggregate)
//	if value, found := cache.Get(userID); found {
//	    // 使用缓存的值。
//	}
type Cache[K comparable, V any] struct {
	name   string
	config Config

	clock clock.IClock
	// statsEnabled 控制是否记录统计信息（Hits/Misses/Evictions/Expires）。
	// 注意：Size 始终可通过 len(items) 计算，因此 Stats().Size 不依赖该开关。
	statsEnabled bool

	// 缓存数据
	items map[K]*cacheEntry[K, V]

	// LRU 链表（最近使用的在前）
	lruList *list.List

	// 并发控制
	mu sync.RWMutex

	// 统计信息
	stats CacheStats
}

// cacheEntry 缓存条目。
type cacheEntry[K comparable, V any] struct {
	key        K
	value      V
	createdAt  time.Time
	accessedAt time.Time
	lruElement *list.Element // LRU 链表中的位置
}

// Config 缓存配置，用于控制存储层缓存的行为（TTL、最大条目数、清理策略等）。
type Config struct {
	// Name 缓存名称（用于日志和统计）
	Name string

	// MaxSize 最大缓存条目数，0 表示无限制（不推荐）
	MaxSize int

	// TTL 缓存过期时间，基于访问时间
	// 0 表示永不过期
	TTL time.Duration

	// Clock 可选：用于 Now()/Since() 等时间来源；便于测试稳定推进时间。
	Clock clock.IClock

	// EnableStats 是否启用统计（默认启用）。
	//
	// 说明：
	// - nil 表示启用（默认）；
	// - false 表示关闭统计（Hits/Misses/Evictions/Expires 始终为 0）。
	EnableStats *bool

	// OnEvict 驱逐回调（可选）
	// 使用 any 以支持不同的键值类型
	OnEvict func(key, value any)
}

// CacheStats 缓存统计信息。
type CacheStats struct {
	Hits      int64 // 缓存命中次数
	Misses    int64 // 缓存未命中次数
	Evictions int64 // LRU 驱逐次数
	Expires   int64 // TTL 过期次数
	Size      int   // 当前条目数
}

// New 创建一个带 LRU 与 TTL 能力的泛型缓存实例。
func New[K comparable, V any](config Config) *Cache[K, V] {
	if config.Name == "" {
		config.Name = "unnamed"
	}
	if config.Clock == nil {
		config.Clock = clock.NewRealClock()
	}

	statsEnabled := true
	if config.EnableStats != nil {
		statsEnabled = *config.EnableStats
	}

	return &Cache[K, V]{
		name:         config.Name,
		config:       config,
		clock:        config.Clock,
		statsEnabled: statsEnabled,
		items:        make(map[K]*cacheEntry[K, V]),
		lruList:      list.New(),
	}
}

// Get 返回缓存值；命中时会刷新访问时间并更新 LRU 顺序。
func (c *Cache[K, V]) Get(key K) (value V, found bool) {
	// 这里使用写锁而不是读锁，是因为 Get 需要更新访问时间、LRU 链表位置以及统计信息，
	// 都会修改内部状态；为了保证 LRU 与统计的一致性，选择在单一写锁下完成读取与更新。
	// 若业务有极端读多写少的高并发场景，可在上层增加只读缓存或副本缓存来分担压力。
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, exists := c.items[key]
	if !exists {
		// 缓存未命中
		if c.statsEnabled {
			c.stats.Misses++
		}
		return value, false
	}

	// 检查是否过期
	if c.isExpired(entry) {
		// 已过期，删除
		c.removeEntryUnsafe(entry)
		if c.statsEnabled {
			c.stats.Misses++
			c.stats.Expires++
		}
		return value, false
	}

	// 缓存命中，更新访问时间和 LRU 位置
	entry.accessedAt = c.clock.Now()
	c.lruList.MoveToFront(entry.lruElement)
	if c.statsEnabled {
		c.stats.Hits++
	}

	return entry.value, true
}

// Set 写入缓存值；容量已满时会先淘汰最久未使用的条目。
func (c *Cache[K, V]) Set(key K, value V) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.clock.Now()

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

// Delete 删除指定 key 的缓存条目，并返回是否真的删除了已有值。
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

// Clear 清空整个缓存，并按需触发驱逐回调。
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

// CleanExpired 主动清理所有已过 TTL 的缓存条目，并返回清理数量。
func (c *Cache[K, V]) CleanExpired() int {
	if c.config.TTL <= 0 {
		return 0 // 永不过期
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	cleaned := 0
	now := c.clock.Now()

	for _, entry := range c.items {
		if now.Sub(entry.accessedAt) >= c.config.TTL {
			c.removeEntryUnsafe(entry)
			cleaned++
		}
	}

	if c.statsEnabled {
		c.stats.Expires += int64(cleaned)
	}
	c.stats.Size = len(c.items)

	return cleaned
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

// isExpired 判断指定条目是否已经超过 TTL；调用方需自行持锁。
func (c *Cache[K, V]) isExpired(entry *cacheEntry[K, V]) bool {
	if c.config.TTL <= 0 {
		return false
	}
	return c.clock.Now().Sub(entry.accessedAt) >= c.config.TTL
}

// evictOldestUnsafe 删除 LRU 链表尾部的最旧条目；调用方需自行持锁。
func (c *Cache[K, V]) evictOldestUnsafe() {
	// 从 LRU 链表尾部获取最久未使用的条目
	oldest := c.lruList.Back()
	if oldest == nil {
		return
	}

	entry := oldest.Value.(*cacheEntry[K, V])
	c.removeEntryUnsafe(entry)
	if c.statsEnabled {
		c.stats.Evictions++
	}
}

// removeEntryUnsafe 从 map 和 LRU 链表中同时移除一个条目；调用方需自行持锁。
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
