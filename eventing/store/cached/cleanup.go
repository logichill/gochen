package cached

import (
	"time"
)

// startCleanupWorker 启动定期清理过期缓存。
func (s *CachedEventStore[ID]) startCleanupWorker(interval time.Duration) {
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

// cleanupExpiredCache 清理过期缓存。
func (s *CachedEventStore[ID]) cleanupExpiredCache() {
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

// ClearCache 清空缓存。
func (s *CachedEventStore[ID]) ClearCache() {
	s.cache.mutex.Lock()
	defer s.cache.mutex.Unlock()

	count := len(s.cache.aggregateCache)
	s.cache.aggregateCache = make(map[string]*CachedAggregate[ID])

	s.stats.mutex.Lock()
	s.stats.Evictions += int64(count)
	s.stats.mutex.Unlock()
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

	if closer, ok := s.store.(interface{ Close() error }); ok {
		return closer.Close()
	}

	return nil
}
