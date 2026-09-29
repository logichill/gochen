package cached

import (
	"container/list"
	"sync"
	"time"

	"gochen/eventing"
	"gochen/messaging"
)

// EventCache 事件缓存。
type EventCache[ID comparable] struct {
	aggregateCache map[aggregateCacheKey[ID]]*CachedAggregate[ID] // 聚合缓存（key: aggregateType + aggregateID）
	lru            *list.List                                     // aggregateCacheKey[ID], front is least recently used
	lruIndex       map[aggregateCacheKey[ID]]*list.Element
	generations    map[aggregateCacheKey[ID]]uint64
	activeFills    map[aggregateCacheKey[ID]]int
	ttl            time.Duration // 缓存过期时间
	maxAggregates  int           // 最大缓存聚合数
	mutex          sync.RWMutex
}

// CachedAggregate 缓存的聚合数据。
type CachedAggregate[ID comparable] struct {
	Events    []eventing.Event[ID] // 事件列表
	Version   uint64               // 当前版本
	CreatedAt time.Time            // 创建时间
}

type aggregateCacheKey[ID comparable] struct {
	aggregateType string
	aggregateID   ID
}

func cacheKey[ID comparable](aggregateType string, aggregateID ID) aggregateCacheKey[ID] {
	return aggregateCacheKey[ID]{aggregateType: aggregateType, aggregateID: aggregateID}
}

// getCachedEvents 从缓存获取事件。
func (s *CachedEventStore[ID]) getCachedEvents(key aggregateCacheKey[ID], fromVersion uint64) []eventing.Event[ID] {
	if s == nil || s.cache == nil {
		return nil
	}
	s.cache.mutex.RLock()
	cached, exists := s.cache.aggregateCache[key]
	if !exists || s.isExpired(cached) {
		s.cache.mutex.RUnlock()
		if exists {
			s.cache.mutex.Lock()
			if current, ok := s.cache.aggregateCache[key]; ok && s.isExpired(current) {
				s.deleteCacheEntryUnsafe(key)
				s.recordEviction()
			}
			s.cache.mutex.Unlock()
		}
		return nil
	}

	var result []eventing.Event[ID]
	if fromVersion == 0 {
		result = append([]eventing.Event[ID](nil), cached.Events...)
	} else {
		for _, evt := range cached.Events {
			if evt.Version > fromVersion {
				result = append(result, evt)
			}
		}
	}
	s.cache.mutex.RUnlock()

	s.cache.mutex.Lock()
	if current, ok := s.cache.aggregateCache[key]; ok && current == cached && !s.isExpired(current) {
		s.touchCacheEntryUnsafe(key)
	}
	s.cache.mutex.Unlock()

	return cloneEvents(result)
}

// cacheAggregate 缓存聚合事件。
func (s *CachedEventStore[ID]) cacheAggregate(key aggregateCacheKey[ID], events []eventing.Event[ID]) {
	if s == nil || s.cache == nil {
		return
	}
	s.cache.mutex.Lock()
	defer s.cache.mutex.Unlock()
	s.cacheAggregateUnsafe(key, events)
}

func (s *CachedEventStore[ID]) cacheAggregateUnsafe(key aggregateCacheKey[ID], events []eventing.Event[ID]) {
	if s == nil || s.cache == nil || len(events) == 0 || s.cache.maxAggregates <= 0 {
		return
	}
	if s.cache.aggregateCache == nil {
		s.cache.aggregateCache = make(map[aggregateCacheKey[ID]]*CachedAggregate[ID])
	}
	if s.cache.lru == nil {
		s.cache.lru = list.New()
	}
	if s.cache.lruIndex == nil {
		s.cache.lruIndex = make(map[aggregateCacheKey[ID]]*list.Element)
	}

	// 获取最新版本
	latestVersion := events[len(events)-1].Version

	now := s.now()

	cached := &CachedAggregate[ID]{
		Events:    cloneEvents(events),
		Version:   latestVersion,
		CreatedAt: now,
	}
	if _, exists := s.cache.aggregateCache[key]; !exists {
		for len(s.cache.aggregateCache) >= s.cache.maxAggregates {
			s.evictOldestUnsafe()
		}
		s.addCacheEntryUnsafe(key)
	} else {
		s.touchCacheEntryUnsafe(key)
	}
	s.cache.aggregateCache[key] = cached
}

func (s *CachedEventStore[ID]) beginCacheFill(key aggregateCacheKey[ID]) uint64 {
	if s == nil || s.cache == nil {
		return 0
	}
	s.cache.mutex.Lock()
	defer s.cache.mutex.Unlock()
	if s.cache.generations == nil {
		s.cache.generations = make(map[aggregateCacheKey[ID]]uint64)
	}
	if s.cache.activeFills == nil {
		s.cache.activeFills = make(map[aggregateCacheKey[ID]]int)
	}
	s.cache.activeFills[key]++
	return s.cache.generations[key]
}

func (s *CachedEventStore[ID]) finishCacheFill(key aggregateCacheKey[ID], events []eventing.Event[ID], generation uint64) {
	if s == nil || s.cache == nil {
		return
	}
	s.cache.mutex.Lock()
	defer s.cache.mutex.Unlock()

	if s.cache.generations[key] == generation {
		s.cacheAggregateUnsafe(key, events)
	}
	if s.cache.activeFills[key] <= 1 {
		delete(s.cache.activeFills, key)
		delete(s.cache.generations, key)
		return
	}
	s.cache.activeFills[key]--
}

// invalidateCache 失效缓存。
func (s *CachedEventStore[ID]) invalidateCache(aggregateType string, aggregateID ID) {
	if s == nil || s.cache == nil {
		return
	}
	s.cache.mutex.Lock()
	defer s.cache.mutex.Unlock()

	key := cacheKey(aggregateType, aggregateID)
	if s.cache.generations == nil {
		s.cache.generations = make(map[aggregateCacheKey[ID]]uint64)
	}
	if s.cache.aggregateCache == nil {
		s.cache.aggregateCache = make(map[aggregateCacheKey[ID]]*CachedAggregate[ID])
	}
	s.cache.generations[key]++
	if _, exists := s.cache.aggregateCache[key]; exists {
		s.deleteCacheEntryUnsafe(key)
		s.recordInvalidation()
	}
	if s.cache.activeFills[key] == 0 {
		delete(s.cache.generations, key)
	}
}

// evictOldestUnsafe 驱逐最旧的缓存项（非线程安全）。
func (s *CachedEventStore[ID]) evictOldestUnsafe() {
	if s == nil || s.cache == nil || s.cache.lru == nil {
		return
	}
	for elem := s.cache.lru.Front(); elem != nil; elem = s.cache.lru.Front() {
		key, _ := elem.Value.(aggregateCacheKey[ID])
		s.cache.lru.Remove(elem)
		delete(s.cache.lruIndex, key)
		if _, exists := s.cache.aggregateCache[key]; !exists {
			continue
		}
		delete(s.cache.aggregateCache, key)
		s.recordEviction()
		return
	}
}

func (s *CachedEventStore[ID]) addCacheEntryUnsafe(key aggregateCacheKey[ID]) {
	if s == nil || s.cache == nil {
		return
	}
	if s.cache.aggregateCache == nil {
		s.cache.aggregateCache = make(map[aggregateCacheKey[ID]]*CachedAggregate[ID])
	}
	if s.cache.lru == nil {
		s.cache.lru = list.New()
	}
	if s.cache.lruIndex == nil {
		s.cache.lruIndex = make(map[aggregateCacheKey[ID]]*list.Element)
	}
	if elem, ok := s.cache.lruIndex[key]; ok {
		s.cache.lru.MoveToBack(elem)
		return
	}
	s.cache.lruIndex[key] = s.cache.lru.PushBack(key)
}

func (s *CachedEventStore[ID]) touchCacheEntryUnsafe(key aggregateCacheKey[ID]) {
	if s == nil || s.cache == nil {
		return
	}
	if _, exists := s.cache.aggregateCache[key]; !exists {
		return
	}
	if elem, ok := s.cache.lruIndex[key]; ok {
		s.cache.lru.MoveToBack(elem)
		return
	}
	s.addCacheEntryUnsafe(key)
}

func (s *CachedEventStore[ID]) deleteCacheEntryUnsafe(key aggregateCacheKey[ID]) {
	if s == nil || s.cache == nil {
		return
	}
	delete(s.cache.aggregateCache, key)
	if elem, ok := s.cache.lruIndex[key]; ok {
		s.cache.lru.Remove(elem)
		delete(s.cache.lruIndex, key)
	}
}

// isExpired 检查缓存是否过期。
func (s *CachedEventStore[ID]) isExpired(cached *CachedAggregate[ID]) bool {
	if s == nil || s.cache == nil || cached == nil {
		return true
	}
	return s.now().Sub(cached.CreatedAt) > s.cache.ttl
}

func (s *CachedEventStore[ID]) now() time.Time {
	if s == nil || isNilCachedDependency(s.clock) {
		return time.Now()
	}
	return s.clock.Now()
}

func cloneEvents[ID comparable](events []eventing.Event[ID]) []eventing.Event[ID] {
	if len(events) == 0 {
		return []eventing.Event[ID]{}
	}
	out := make([]eventing.Event[ID], len(events))
	for i := range events {
		out[i] = cloneEvent(events[i])
	}
	return out
}

func cloneEvent[ID comparable](evt eventing.Event[ID]) eventing.Event[ID] {
	clone := evt
	clone.Message = messaging.CloneMessage(evt.Message)
	return clone
}
