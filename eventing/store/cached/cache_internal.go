package cached

import (
	"fmt"
	"sync"
	"time"

	"gochen/eventing"
)

// EventCache 事件缓存。
type EventCache[ID comparable] struct {
	aggregateCache map[string]*CachedAggregate[ID] // 聚合缓存（key: aggregate_type:aggregate_id）
	ttl            time.Duration                   // 缓存过期时间
	maxAggregates  int                             // 最大缓存聚合数
	mutex          sync.RWMutex
}

// CachedAggregate 缓存的聚合数据。
type CachedAggregate[ID comparable] struct {
	Events     []eventing.Event[ID] // 事件列表
	Version    uint64               // 当前版本
	LastAccess time.Time            // 最后访问时间
	CreatedAt  time.Time            // 创建时间
}

func cacheKey[ID comparable](aggregateType string, aggregateID ID) string {
	return fmt.Sprintf("%s:%v", aggregateType, aggregateID)
}

// getCachedEvents 从缓存获取事件。
func (s *CachedEventStore[ID]) getCachedEvents(key string, fromVersion uint64) []eventing.Event[ID] {
	s.cache.mutex.RLock()
	cached, exists := s.cache.aggregateCache[key]
	if !exists || s.isExpired(cached) {
		s.cache.mutex.RUnlock()
		return nil
	}

	// 在读锁下复制事件数据，避免与写操作竞争
	var result []eventing.Event[ID]
	if fromVersion == 0 {
		result = make([]eventing.Event[ID], len(cached.Events))
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

// cacheAggregate 缓存聚合事件。
func (s *CachedEventStore[ID]) cacheAggregate(key string, events []eventing.Event[ID]) {
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
	cachedEvents := make([]eventing.Event[ID], len(events))
	copy(cachedEvents, events)

	s.cache.aggregateCache[key] = &CachedAggregate[ID]{
		Events:     cachedEvents,
		Version:    latestVersion,
		LastAccess: time.Now(),
		CreatedAt:  time.Now(),
	}
}

// invalidateCache 失效缓存。
func (s *CachedEventStore[ID]) invalidateCache(aggregateType string, aggregateID ID) {
	s.cache.mutex.Lock()
	defer s.cache.mutex.Unlock()

	key := cacheKey(aggregateType, aggregateID)
	if _, exists := s.cache.aggregateCache[key]; exists {
		delete(s.cache.aggregateCache, key)
		s.recordInvalidation()
	}
}

// evictOldestUnsafe 驱逐最旧的缓存项（非线程安全）。
func (s *CachedEventStore[ID]) evictOldestUnsafe() {
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

// isExpired 检查缓存是否过期。
func (s *CachedEventStore[ID]) isExpired(cached *CachedAggregate[ID]) bool {
	return time.Since(cached.CreatedAt) > s.cache.ttl
}
