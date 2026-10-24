package cached

import (
	"container/list"
	"time"

	"gochen/eventing"
)

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
			removed := false
			if current, ok := s.cache.aggregateCache[key]; ok && s.isExpired(current) {
				s.deleteCacheEntryUnsafe(key)
				removed = true
			}
			s.cache.mutex.Unlock()
			// 指标记录在释放缓存写锁之后，避免持锁调用外部记录器。
			if removed {
				s.recordEviction()
			}
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
	evicted := s.cacheAggregateUnsafe(key, events)
	s.cache.mutex.Unlock()
	// 指标记录在释放缓存写锁之后，避免持锁调用外部记录器。
	if evicted > 0 {
		s.recordEvictions(evicted)
	}
}

// cacheAggregateUnsafe 在持有 s.cache.mutex 的前提下写入缓存，
// 返回本次因容量上限被驱逐的条目数（由调用方在解锁后记录指标）。
func (s *CachedEventStore[ID]) cacheAggregateUnsafe(key aggregateCacheKey[ID], events []eventing.Event[ID]) int {
	if s == nil || s.cache == nil || len(events) == 0 || s.cache.maxAggregates <= 0 {
		return 0
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
	evicted := 0
	if _, exists := s.cache.aggregateCache[key]; !exists {
		for len(s.cache.aggregateCache) >= s.cache.maxAggregates {
			// evictOldestUnsafe 未能实际淘汰（例如聚合缓存与 LRU 索引不一致）
			// 时防御性中断，避免持写锁死循环。
			if !s.evictOldestUnsafe() {
				break
			}
			evicted++
		}
		s.addCacheEntryUnsafe(key)
	} else {
		s.touchCacheEntryUnsafe(key)
	}
	s.cache.aggregateCache[key] = cached
	return evicted
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
	evicted := 0
	if s.cache.generations[key] == generation {
		evicted = s.cacheAggregateUnsafe(key, events)
	}
	if s.cache.activeFills[key] <= 1 {
		delete(s.cache.activeFills, key)
		delete(s.cache.generations, key)
	} else {
		s.cache.activeFills[key]--
	}
	s.cache.mutex.Unlock()
	if evicted > 0 {
		s.recordEvictions(evicted)
	}
}

// invalidateCache 失效缓存。
func (s *CachedEventStore[ID]) invalidateCache(aggregateType string, aggregateID ID) {
	if s == nil || s.cache == nil {
		return
	}
	s.cache.mutex.Lock()
	defer s.cache.mutex.Unlock()

	if s.cache.generations == nil {
		s.cache.generations = make(map[aggregateCacheKey[ID]]uint64)
	}
	// 一条事件流可以有多个租户视图，追加后须一起失效，包括尚未完成的回填。
	for key := range s.cache.aggregateCache {
		if key.aggregateType == aggregateType && key.aggregateID == aggregateID {
			s.deleteCacheEntryUnsafe(key)
			s.recordInvalidation()
		}
	}
	for key := range s.cache.activeFills {
		if key.aggregateType == aggregateType && key.aggregateID == aggregateID {
			s.cache.generations[key]++
		}
	}
}

// evictOldestUnsafe 驱逐最旧的缓存项（非线程安全）。
// 返回是否真的淘汰了一个聚合缓存项；无法淘汰时由调用方防御性中断淘汰循环。
//
// 本函数不记录指标：调用方持有 s.cache.mutex，而指标记录器是外部实现，
// 持锁调用有死锁风险，故由调用方在解锁后统一记录。
func (s *CachedEventStore[ID]) evictOldestUnsafe() bool {
	if s == nil || s.cache == nil || s.cache.lru == nil {
		return false
	}
	for elem := s.cache.lru.Front(); elem != nil; elem = s.cache.lru.Front() {
		key, _ := elem.Value.(aggregateCacheKey[ID])
		s.cache.lru.Remove(elem)
		delete(s.cache.lruIndex, key)
		if _, exists := s.cache.aggregateCache[key]; !exists {
			continue
		}
		delete(s.cache.aggregateCache, key)
		return true
	}
	return false
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
