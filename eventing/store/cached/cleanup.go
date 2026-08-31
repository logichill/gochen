package cached

import (
	"container/list"
	"time"

	"gochen/clock"
)

// startCleanupWorker 启动定期清理过期缓存。
func (s *CachedEventStore[ID]) startCleanupWorker(interval time.Duration) {
	if s == nil || s.cache == nil || s.stopCh == nil {
		return
	}
	ticker, err := s.newTicker(interval)
	if err != nil {
		// 自定义 clock 创建 ticker 失败时回退到真实时钟，
		// 避免清理协程静默不启动导致过期缓存无限增长。
		ticker, err = clock.NewRealClock().NewTicker(interval)
		if err != nil {
			return
		}
	}
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C():
			s.cleanupExpiredCache()
		case <-s.stopCh:
			return
		}
	}
}

// cleanupExpiredCache 清理过期缓存。
func (s *CachedEventStore[ID]) cleanupExpiredCache() {
	if s == nil || s.cache == nil {
		return
	}
	s.cache.mutex.Lock()
	defer s.cache.mutex.Unlock()

	for key, cached := range s.cache.aggregateCache {
		if s.isExpired(cached) {
			s.deleteCacheEntryUnsafe(key)
			s.recordEviction()
		}
	}
}

// ClearCache 清空缓存。
func (s *CachedEventStore[ID]) ClearCache() {
	if s == nil || s.cache == nil {
		return
	}
	s.cache.mutex.Lock()
	defer s.cache.mutex.Unlock()

	if s.cache.generations == nil {
		s.cache.generations = make(map[aggregateCacheKey[ID]]uint64)
	}
	for key, active := range s.cache.activeFills {
		if active > 0 {
			s.cache.generations[key]++
		}
	}

	count := len(s.cache.aggregateCache)
	s.cache.aggregateCache = make(map[aggregateCacheKey[ID]]*CachedAggregate[ID])
	s.cache.lru = list.New()
	s.cache.lruIndex = make(map[aggregateCacheKey[ID]]*list.Element)

	if s.stats != nil {
		s.stats.mutex.Lock()
		s.stats.Evictions += int64(count)
		s.stats.mutex.Unlock()
	}
}

// Close 关闭并释放资源。
//
// 说明：
// - Close 释放缓存存储相关资源（停止后台清理协程）
func (s *CachedEventStore[ID]) Close() error {
	if s == nil {
		return nil
	}

	s.stopOnce.Do(func() {
		if s.stopCh != nil {
			close(s.stopCh)
		}
	})

	if !isNilCachedDependency(s.store) {
		if closer, ok := s.store.(interface{ Close() error }); ok {
			return closer.Close()
		}
	}

	return nil
}

func (s *CachedEventStore[ID]) newTicker(interval time.Duration) (clock.ITicker, error) {
	if s == nil || isNilCachedDependency(s.clock) {
		return clock.NewRealClock().NewTicker(interval)
	}
	return s.clock.NewTicker(interval)
}
