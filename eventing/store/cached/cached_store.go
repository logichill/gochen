package cached

import (
	"context"
	"fmt"
	"sync"
	"time"

	"gochen/eventing"
	"gochen/eventing/monitoring"
	"gochen/eventing/store"
)

// CachedEventStore 带缓存的事件存储装饰器
// 使用缓存提升事件读取性能，适合读多写少的场景
type CachedEventStore struct {
	store store.IEventStore[int64] // 底层事件存储（当前实现针对 int64 聚合 ID）
	cache *EventCache              // 缓存层
	stats *CacheStats              // 缓存统计

	stopCh chan struct{}
	// 关闭标识用于避免重复关闭
	stopOnce sync.Once
}

// EventCache 事件缓存
type EventCache struct {
	aggregateCache map[string]*CachedAggregate // 聚合缓存（key: aggregate_type:aggregate_id）
	ttl            time.Duration               // 缓存过期时间
	maxAggregates  int                         // 最大缓存聚合数
	mutex          sync.RWMutex
}

// CachedAggregate 缓存的聚合数据
type CachedAggregate struct {
	Events     []eventing.Event[int64] // 事件列表
	Version    uint64                 // 当前版本
	LastAccess time.Time              // 最后访问时间
	CreatedAt  time.Time              // 创建时间
}

func cacheKey(aggregateType string, aggregateID int64) string {
	return fmt.Sprintf("%s:%d", aggregateType, aggregateID)
}

// CacheStats 缓存统计信息
type CacheStats struct {
	Hits          int64 // 缓存命中次数
	Misses        int64 // 缓存未命中次数
	Evictions     int64 // 缓存驱逐次数
	Invalidations int64 // 缓存失效次数
	mutex         sync.RWMutex
}

// Config 缓存配置
type Config struct {
	TTL             time.Duration // 缓存过期时间（默认: 5分钟）
	MaxAggregates   int           // 最大缓存聚合数（默认: 1000）
	CleanupInterval time.Duration // 清理间隔（默认: 1分钟）
}

// DefaultConfig 默认缓存配置
func DefaultConfig() *Config {
	return &Config{
		TTL:             5 * time.Minute,
		MaxAggregates:   1000,
		CleanupInterval: 1 * time.Minute,
	}
}

// NewCachedEventStore 创建带缓存的事件存储（当前针对 int64 聚合 ID 的 IEventStore[int64]）
func NewCachedEventStore(store store.IEventStore[int64], config *Config) *CachedEventStore {
	if config == nil {
		config = DefaultConfig()
	}

	cache := &EventCache{
		aggregateCache: make(map[string]*CachedAggregate),
		ttl:            config.TTL,
		maxAggregates:  config.MaxAggregates,
	}

	cached := &CachedEventStore{
		store:  store,
		cache:  cache,
		stats:  &CacheStats{},
		stopCh: make(chan struct{}),
	}

	// 启动定期清理过期缓存
	go cached.startCleanupWorker(config.CleanupInterval)

	return cached
}

// AppendEvents 保存事件并失效缓存
func (s *CachedEventStore) AppendEvents(ctx context.Context, aggregateID int64, events []eventing.IStorableEvent[int64], expectedVersion uint64) error {
	// 写入底层存储
	if err := s.store.AppendEvents(ctx, aggregateID, events, expectedVersion); err != nil {
		return err
	}

	// 失效缓存
	aggregateType := ""
	if len(events) > 0 {
		aggregateType = events[0].GetAggregateType()
	}
	s.invalidateCache(aggregateType, aggregateID)
	s.invalidateCache("", aggregateID)

	return nil
}

// LoadEvents 加载聚合的事件（优先从缓存）
func (s *CachedEventStore) LoadEvents(ctx context.Context, aggregateID int64, afterVersion uint64) ([]eventing.Event[int64], error) {
	// 尝试从缓存获取
	if cached := s.getCachedEvents(cacheKey("", aggregateID), afterVersion); cached != nil {
		s.recordHit()
		return cached, nil
	}

	s.recordMiss()

	// 缓存未命中，从底层存储加载
	events, err := s.store.LoadEvents(ctx, aggregateID, afterVersion)
	if err != nil {
		return nil, err
	}

	// 如果 afterVersion 为 0，表示加载全部事件，可以缓存
	if afterVersion == 0 && len(events) > 0 {
		s.cacheAggregate(cacheKey("", aggregateID), events)
	}

	return events, nil
}

// LoadEventsByType 加载指定聚合类型的事件（优先从缓存）
func (s *CachedEventStore) LoadEventsByType(ctx context.Context, aggregateType string, aggregateID int64, afterVersion uint64) ([]eventing.Event[int64], error) {
	key := cacheKey(aggregateType, aggregateID)
	if cached := s.getCachedEvents(key, afterVersion); cached != nil {
		s.recordHit()
		return cached, nil
	}

	s.recordMiss()

	var (
		events []eventing.Event[int64]
		err    error
	)
	if typedStore, ok := s.store.(store.ITypedEventStore[int64]); ok {
		events, err = typedStore.LoadEventsByType(ctx, aggregateType, aggregateID, afterVersion)
	} else {
		// 回退到通用加载并按类型过滤
		allEvents, loadErr := s.store.LoadEvents(ctx, aggregateID, afterVersion)
		if loadErr != nil {
			return nil, loadErr
		}
		for _, evt := range allEvents {
			if evt.AggregateType == aggregateType {
				events = append(events, evt)
			}
		}
	}

	if err != nil {
		return nil, err
	}

	if afterVersion == 0 && len(events) > 0 {
		s.cacheAggregate(key, events)
	}

	return events, nil
}

// StreamEvents 获取事件流（不缓存）
func (s *CachedEventStore) StreamEvents(ctx context.Context, fromTimestamp time.Time) ([]eventing.Event[int64], error) {
	return s.store.StreamEvents(ctx, fromTimestamp)
}

// GetEventStreamWithCursor 基于游标/类型过滤读取事件流（如底层支持则委托，否则回退过滤）
func (s *CachedEventStore) GetEventStreamWithCursor(ctx context.Context, opts *store.StreamOptions) (*store.StreamResult[int64], error) {
	if extended, ok := s.store.(store.IEventStoreExtended[int64]); ok {
		return extended.GetEventStreamWithCursor(ctx, opts)
	}

	events, err := s.store.StreamEvents(ctx, opts.FromTime)
	if err != nil {
		return nil, err
	}
	result := store.FilterEventsWithOptions[int64](events, opts)
	return result, nil
}

// getCachedEvents 从缓存获取事件
func (s *CachedEventStore) getCachedEvents(key string, fromVersion uint64) []eventing.Event[int64] {
	s.cache.mutex.RLock()
	cached, exists := s.cache.aggregateCache[key]
	if !exists || s.isExpired(cached) {
		s.cache.mutex.RUnlock()
		return nil
	}

	// 在读锁下复制事件数据，避免与写操作竞争
	var result []eventing.Event[int64]
	if fromVersion == 0 {
		result = make([]eventing.Event[int64], len(cached.Events))
		copy(result, cached.Events)
	} else {
		for _, evt := range cached.Events {
			if evt.Version > fromVersion {
				result = append(result, evt)
			}
		}
	}
	s.cache.mutex.RUnlock()

	// 单独获取写锁更新 LastAccess，避免在读锁下写入
	s.cache.mutex.Lock()
	if cachedLatest, ok := s.cache.aggregateCache[key]; ok {
		cachedLatest.LastAccess = time.Now()
	}
	s.cache.mutex.Unlock()

	return result
}

// cacheAggregate 缓存聚合事件
func (s *CachedEventStore) cacheAggregate(key string, events []eventing.Event[int64]) {
	if len(events) == 0 {
		return
	}

	s.cache.mutex.Lock()
	defer s.cache.mutex.Unlock()

	// 检查缓存大小，必要时驱逐
	if len(s.cache.aggregateCache) >= s.cache.maxAggregates {
		s.evictOldestUnsafe()
	}

	// 获取最新版本
	latestVersion := events[len(events)-1].Version

	// 创建缓存副本
	cachedEvents := make([]eventing.Event[int64], len(events))
	copy(cachedEvents, events)

	s.cache.aggregateCache[key] = &CachedAggregate{
		Events:     cachedEvents,
		Version:    latestVersion,
		LastAccess: time.Now(),
		CreatedAt:  time.Now(),
	}
}

// invalidateCache 失效缓存
func (s *CachedEventStore) invalidateCache(aggregateType string, aggregateID int64) {
	s.cache.mutex.Lock()
	defer s.cache.mutex.Unlock()

	key := cacheKey(aggregateType, aggregateID)
	if _, exists := s.cache.aggregateCache[key]; exists {
		delete(s.cache.aggregateCache, key)
		s.recordInvalidation()
	}
}

// evictOldestUnsafe 驱逐最旧的缓存项（非线程安全）
func (s *CachedEventStore) evictOldestUnsafe() {
	var oldestKey string
	var oldestTime time.Time

	// 查找最久未访问的聚合
	for key, cached := range s.cache.aggregateCache {
		if oldestTime.IsZero() || cached.LastAccess.Before(oldestTime) {
			oldestTime = cached.LastAccess
			oldestKey = key
		}
	}

	if !oldestTime.IsZero() {
		delete(s.cache.aggregateCache, oldestKey)
		s.recordEviction()
	}
}

// isExpired 检查缓存是否过期
func (s *CachedEventStore) isExpired(cached *CachedAggregate) bool {
	return time.Since(cached.CreatedAt) > s.cache.ttl
}

// startCleanupWorker 启动定期清理过期缓存
func (s *CachedEventStore) startCleanupWorker(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.cleanupExpiredCache()
		case <-s.stopCh:
			return
		}
	}
}

// cleanupExpiredCache 清理过期缓存
func (s *CachedEventStore) cleanupExpiredCache() {
	s.cache.mutex.Lock()
	defer s.cache.mutex.Unlock()

	now := time.Now()
	for key, cached := range s.cache.aggregateCache {
		if now.Sub(cached.CreatedAt) > s.cache.ttl {
			delete(s.cache.aggregateCache, key)
			s.recordEviction()
		}
	}
}

// ClearCache 清空缓存
func (s *CachedEventStore) ClearCache() {
	s.cache.mutex.Lock()
	defer s.cache.mutex.Unlock()

	count := len(s.cache.aggregateCache)
	s.cache.aggregateCache = make(map[string]*CachedAggregate)

	s.stats.mutex.Lock()
	s.stats.Evictions += int64(count)
	s.stats.mutex.Unlock()
}

// GetCacheStats 获取缓存统计信息
func (s *CachedEventStore) GetCacheStats() map[string]any {
	s.stats.mutex.RLock()
	defer s.stats.mutex.RUnlock()

	s.cache.mutex.RLock()
	cacheSize := len(s.cache.aggregateCache)
	s.cache.mutex.RUnlock()

	totalRequests := s.stats.Hits + s.stats.Misses
	hitRate := 0.0
	if totalRequests > 0 {
		hitRate = float64(s.stats.Hits) / float64(totalRequests) * 100
	}

	return map[string]any{
		"hits":          s.stats.Hits,
		"misses":        s.stats.Misses,
		"hit_rate":      fmt.Sprintf("%.2f%%", hitRate),
		"evictions":     s.stats.Evictions,
		"invalidations": s.stats.Invalidations,
		"cache_size":    cacheSize,
		"max_size":      s.cache.maxAggregates,
		"ttl_seconds":   s.cache.ttl.Seconds(),
	}
}

// recordHit 记录缓存命中
func (s *CachedEventStore) recordHit() {
	s.stats.mutex.Lock()
	defer s.stats.mutex.Unlock()
	s.stats.Hits++
	monitoring.GlobalMetrics().RecordCacheHit()
}

// recordMiss 记录缓存未命中
func (s *CachedEventStore) recordMiss() {
	s.stats.mutex.Lock()
	defer s.stats.mutex.Unlock()
	s.stats.Misses++
	monitoring.GlobalMetrics().RecordCacheMiss()
}

// recordEviction 记录缓存驱逐
func (s *CachedEventStore) recordEviction() {
	s.stats.mutex.Lock()
	defer s.stats.mutex.Unlock()
	s.stats.Evictions++
	monitoring.GlobalMetrics().RecordCacheEviction()
}

// recordInvalidation 记录缓存失效
func (s *CachedEventStore) recordInvalidation() {
	s.stats.mutex.Lock()
	defer s.stats.mutex.Unlock()
	s.stats.Invalidations++
}

// GetStats 获取缓存统计信息（简化版）
func (s *CachedEventStore) GetStats() *CacheStats {
	s.stats.mutex.RLock()
	defer s.stats.mutex.RUnlock()

	return &CacheStats{
		Hits:          s.stats.Hits,
		Misses:        s.stats.Misses,
		Evictions:     s.stats.Evictions,
		Invalidations: s.stats.Invalidations,
	}
}

// GetHitRate 获取缓存命中率
func (s *CachedEventStore) GetHitRate() float64 {
	s.stats.mutex.RLock()
	defer s.stats.mutex.RUnlock()

	totalRequests := s.stats.Hits + s.stats.Misses
	if totalRequests == 0 {
		return 0.0
	}

	return float64(s.stats.Hits) / float64(totalRequests)
}

// Close 释放缓存存储相关资源（停止后台清理协程）
func (s *CachedEventStore) Close() error {
	if s == nil {
		return nil
	}

	s.stopOnce.Do(func() {
		if s.stopCh != nil {
			close(s.stopCh)
		}
	})

	if closer, ok := s.store.(interface{ Close() error }); ok {
		return closer.Close()
	}

	return nil
}

// 接口断言
var _ store.IEventStoreExtended[int64] = (*CachedEventStore)(nil)
