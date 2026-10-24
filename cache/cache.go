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
	"sync"
	"time"

	"gochen/clock"
)

// ICache 泛型缓存契约接口。
type ICache[K comparable, V any] interface {
	Get(key K) (value V, found bool)
	Set(key K, value V)
	Delete(key K) bool
	Clear()
	CleanExpired() int
	Stats() CacheStats
	Size() int
	HitRate() float64
}

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

	// lastSweepScanned 最近一次 Set 机会式清扫实际扫描的条目数（持锁写入，
	// 由 lastOpportunisticSweepScans 持锁读取；仅供包内测试观测扫描截断）。
	lastSweepScanned int

	// 后台清理协程的生命周期状态（lifecycleMu 保护）：
	// stopCh 在 Close 时关闭并由下一次 StartCleanup 重建，cleanupDone 在
	// 清理协程退出时关闭；因此 Close→StartCleanup→Close 序列可安全重复
	//（审计 P2#20）。不用 sync.WaitGroup：Add/Wait 无法在重启语义下串行化。
	lifecycleMu    sync.Mutex
	stopCh         chan struct{}
	cleanupRunning bool
	cleanupDone    chan struct{}
}

// cacheEntry 缓存条目。
type cacheEntry[K comparable, V any] struct {
	key        K
	value      V
	createdAt  time.Time
	accessedAt time.Time
	lruElement *list.Element // LRU 链表中的位置
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

// StartCleanup 启动后台过期清理协程（幂等）。
//
// 仅在 TTL > 0 且 CleanupInterval > 0 时实际启动；清理协程已在运行时为空操作。
// Close 之后再次调用会重新启动清理协程（重启语义）：生命周期状态由单一
// lifecycleMu 保护，并发 Start/Close 串行化，Close→Start→Close 序列不会
// 死锁或泄漏清理协程（审计 P2#20 056f2b0a）。
func (c *Cache[K, V]) StartCleanup() {
	if c == nil || c.config.TTL <= 0 || c.config.CleanupInterval <= 0 {
		return
	}
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()
	if c.cleanupRunning {
		return
	}
	stopCh := make(chan struct{})
	done := make(chan struct{})
	c.stopCh = stopCh
	c.cleanupDone = done
	c.cleanupRunning = true
	go func() {
		defer close(done)
		c.cleanupLoop(stopCh)
	}()
}

// Close 停止后台清理协程并等待其退出（幂等且不阻塞在已停止的实例上）。
//
// 语义说明：Close 采用"可重启"语义——Close 只停止当前运行的清理协程，
// 之后可再次调用 StartCleanup 重新启动；未启动时 Close 为空操作。
func (c *Cache[K, V]) Close() {
	if c == nil {
		return
	}
	c.lifecycleMu.Lock()
	if !c.cleanupRunning {
		c.lifecycleMu.Unlock()
		return
	}
	stopCh := c.stopCh
	done := c.cleanupDone
	c.cleanupRunning = false
	c.lifecycleMu.Unlock()

	if stopCh != nil {
		close(stopCh)
	}
	if done != nil {
		<-done
	}
}

func (c *Cache[K, V]) cleanupLoop(stopCh <-chan struct{}) {
	ticker := time.NewTicker(c.config.CleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			c.CleanExpired()
		case <-stopCh:
			return
		}
	}
}

// Get 返回缓存值；命中时会刷新访问时间并更新 LRU 顺序。
//
// 说明：过期条目会先从缓存中移除，再在锁外触发 OnEvict；若 OnEvict panic，
// Get 会与其他驱逐路径一致重新抛出首个 panic。
func (c *Cache[K, V]) Get(key K) (value V, found bool) {
	// 这里使用写锁而不是读锁，是因为 Get 需要更新访问时间、LRU 链表位置以及统计信息，
	// 都会修改内部状态；为了保证 LRU 与统计的一致性，选择在单一写锁下完成读取与更新。
	// 若业务有极端读多写少的高并发场景，可在上层增加只读缓存或副本缓存来分担压力。
	c.mu.Lock()

	entry, exists := c.items[key]
	if !exists {
		// 缓存未命中
		if c.statsEnabled {
			c.stats.Misses++
		}
		c.mu.Unlock()
		return value, false
	}

	// 检查是否过期
	if c.isExpired(entry) {
		// 已过期，删除
		evicted := c.removeEntryUnsafe(entry)
		if c.statsEnabled {
			c.stats.Misses++
			c.stats.Expires++
		}
		c.mu.Unlock()
		c.notifyEvictions([]eviction[K, V]{evicted})
		return value, false
	}

	// 缓存命中，更新访问时间和 LRU 位置
	entry.accessedAt = c.clock.Now()
	c.lruList.MoveToFront(entry.lruElement)
	if c.statsEnabled {
		c.stats.Hits++
	}

	value = entry.value
	c.mu.Unlock()
	return value, true
}

// Set 写入缓存值；容量已满时会先淘汰最久未使用的条目。
func (c *Cache[K, V]) Set(key K, value V) {
	c.mu.Lock()

	now := c.clock.Now()
	var evicted []eviction[K, V]

	// 检查是否已存在
	if entry, exists := c.items[key]; exists {
		// 更新现有条目
		entry.value = value
		entry.accessedAt = now
		c.lruList.MoveToFront(entry.lruElement)
		c.mu.Unlock()
		return
	}

	// 检查是否需要驱逐
	if c.config.MaxSize > 0 && len(c.items) >= c.config.MaxSize {
		if ev, ok := c.evictOldestUnsafe(); ok {
			evicted = append(evicted, ev)
		}
	} else if c.config.MaxSize <= 0 && c.config.TTL > 0 {
		// 无上限且启用 TTL 时，Set 顺带回收已过期条目，
		// 避免“写入热、读取冷”的 key 永久驻留内存。
		// 按扫描条目数截断（上限 maxOpportunisticSweep）：无论本次实际驱逐
		// 多少，单次 Set 持写锁的扫描量都有界，与缓存总规模无关（审计
		// P2#19 7a1eb1b8——旧实现按驱逐数截断，无过期项时扫描不设防，
		// 每次 Set 退化为 O(n) 全表扫）。截断是抽样式的：map 遍历顺序随机，
		// 未扫到的过期条目留给后续 Set 或后台清理兜底。
		scanned := 0
		for _, entry := range c.items {
			if scanned >= maxOpportunisticSweep {
				break
			}
			scanned++
			if c.isExpiredLocked(entry) {
				evicted = append(evicted, c.removeEntryUnsafe(entry))
			}
		}
		// 记录最近一次机会式清扫的实际扫描量，供包内测试断言截断语义。
		c.lastSweepScanned = scanned
		if c.statsEnabled && len(evicted) > 0 {
			c.stats.Expires += int64(len(evicted))
		}
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
	c.mu.Unlock()
	c.notifyEvictions(evicted)
}

// Delete 删除指定 key 的缓存条目，并返回是否真的删除了已有值。
func (c *Cache[K, V]) Delete(key K) bool {
	c.mu.Lock()

	entry, exists := c.items[key]
	if !exists {
		c.mu.Unlock()
		return false
	}

	evicted := c.removeEntryUnsafe(entry)
	c.mu.Unlock()
	c.notifyEvictions([]eviction[K, V]{evicted})
	return true
}

// Clear 清空整个缓存，并按需触发驱逐回调。
//
// 说明：
// - 若配置了 OnEvict，回调按 LRU 逆序触发（最久未使用的条目优先）。
func (c *Cache[K, V]) Clear() {
	c.mu.Lock()

	var evicted []eviction[K, V]
	if c.config.OnEvict != nil {
		evicted = make([]eviction[K, V], 0, len(c.items))
		for elem := c.lruList.Back(); elem != nil; elem = elem.Prev() {
			entry := elem.Value.(*cacheEntry[K, V])
			evicted = append(evicted, eviction[K, V]{key: entry.key, value: entry.value})
		}
	}
	c.items = make(map[K]*cacheEntry[K, V])
	c.lruList = list.New()
	c.stats.Size = 0
	c.mu.Unlock()
	c.notifyEvictions(evicted)
}
