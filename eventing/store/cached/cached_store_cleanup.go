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
//
// 锁边界：只在本函数内完成扫描与删除，外部指标记录器（recordEvictions）
// 必须在释放 s.cache.mutex 之后调用。recordEviction 会调用外部
// ICacheMetricsRecorder，若持锁调用，记录器一旦阻塞或反向访问存储就会
// 死锁/长时间占用写锁，阻塞全部缓存读写。
func (s *CachedEventStore[ID]) cleanupExpiredCache() {
	if s == nil || s.cache == nil {
		return
	}
	evicted := 0
	s.cache.mutex.Lock()
	for key, cached := range s.cache.aggregateCache {
		if s.isExpired(cached) {
			s.deleteCacheEntryUnsafe(key)
			evicted++
		}
	}
	s.cache.mutex.Unlock()

	if evicted > 0 {
		s.recordEvictions(evicted)
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
// - Close 释放缓存存储相关资源（停止后台清理协程），并关闭底层存储；
// - 全部收尾纳入 stopOnce，保证并发与多次 Close 的幂等：底层连接池/驱动的
// Close 不保证可重入，double-close 可能引发报错或 panic；首次错误被缓存并返回。
func (s *CachedEventStore[ID]) Close() error {
	if s == nil {
		return nil
	}

	var closeErr error
	s.stopOnce.Do(func() {
		if s.stopCh != nil {
			close(s.stopCh)
		}
		// 等待后台清理协程退出后再关闭底层存储：否则清理协程可能正在
		// 访问已关闭的连接/缓存，产生 use-after-close 竞态。
		s.cleanupWG.Wait()
		if !isNilCachedDependency(s.store) {
			if closer, ok := s.store.(interface{ Close() error }); ok {
				closeErr = closer.Close()
			}
		}
	})
	return closeErr
}

func (s *CachedEventStore[ID]) newTicker(interval time.Duration) (clock.ITicker, error) {
	if s == nil || isNilCachedDependency(s.clock) {
		return clock.NewRealClock().NewTicker(interval)
	}
	return s.clock.NewTicker(interval)
}

// StartCleanupWorker 显式启动后台清理协程。
func (s *CachedEventStore[ID]) StartCleanupWorker() {
	if s == nil {
		return
	}
	s.startOnce.Do(func() {
		s.cleanupWG.Add(1)
		go func() {
			defer s.cleanupWG.Done()
			s.startCleanupWorker(s.interval)
		}()
	})
}
