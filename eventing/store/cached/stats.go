package cached

import (
	"sync"

	"gochen/eventing/monitoring"
)

// CacheStats 缓存统计信息。
type CacheStats struct {
	Hits          int64 // 缓存命中次数
	Misses        int64 // 缓存未命中次数
	Evictions     int64 // 缓存驱逐次数
	Invalidations int64 // 缓存失效次数
	mutex         sync.RWMutex
}

func (s *CachedEventStore[ID]) CacheStats() monitoring.CacheStats {
	if s == nil {
		return monitoring.CacheStats{}
	}

	s.stats.mutex.RLock()
	hits := s.stats.Hits
	misses := s.stats.Misses
	evictions := s.stats.Evictions
	invalidations := s.stats.Invalidations
	s.stats.mutex.RUnlock()

	s.cache.mutex.RLock()
	cacheSize := len(s.cache.aggregateCache)
	maxSize := s.cache.maxAggregates
	ttlSeconds := s.cache.ttl.Seconds()
	s.cache.mutex.RUnlock()

	total := hits + misses
	hitRate := 0.0
	if total > 0 {
		hitRate = float64(hits) / float64(total) * 100
	}

	return monitoring.CacheStats{
		Hits:           hits,
		Misses:         misses,
		HitRatePercent: hitRate,
		Evictions:      evictions,
		Invalidations:  invalidations,
		CacheSize:      cacheSize,
		MaxSize:        maxSize,
		TTLSeconds:     ttlSeconds,
	}
}

// recordHit 记录缓存命中。
func (s *CachedEventStore[ID]) recordHit() {
	var m monitoring.ICacheMetricsRecorder
	s.stats.mutex.Lock()
	s.stats.Hits++
	m = s.getMetrics()
	s.stats.mutex.Unlock()

	if m != nil {
		m.RecordCacheHit()
	}
}

// recordMiss 记录缓存未命中。
func (s *CachedEventStore[ID]) recordMiss() {
	var m monitoring.ICacheMetricsRecorder
	s.stats.mutex.Lock()
	s.stats.Misses++
	m = s.getMetrics()
	s.stats.mutex.Unlock()

	if m != nil {
		m.RecordCacheMiss()
	}
}

// recordEviction 记录缓存驱逐。
func (s *CachedEventStore[ID]) recordEviction() {
	var m monitoring.ICacheMetricsRecorder
	s.stats.mutex.Lock()
	s.stats.Evictions++
	m = s.getMetrics()
	s.stats.mutex.Unlock()

	if m != nil {
		m.RecordCacheEviction()
	}
}

// recordInvalidation 记录缓存失效。
func (s *CachedEventStore[ID]) recordInvalidation() {
	s.stats.mutex.Lock()
	defer s.stats.mutex.Unlock()
	s.stats.Invalidations++
}

// Stats 从存储中查询数据。
//
// 说明：
// - Stats 获取缓存统计信息（简化版）
func (s *CachedEventStore[ID]) Stats() *CacheStats {
	s.stats.mutex.RLock()
	defer s.stats.mutex.RUnlock()

	return &CacheStats{
		Hits:          s.stats.Hits,
		Misses:        s.stats.Misses,
		Evictions:     s.stats.Evictions,
		Invalidations: s.stats.Invalidations,
	}
}

// GetHitRate 从存储中查询数据。
//
// 说明：
// - GetHitRate 获取缓存命中率。
func (s *CachedEventStore[ID]) GetHitRate() float64 {
	s.stats.mutex.RLock()
	defer s.stats.mutex.RUnlock()

	totalRequests := s.stats.Hits + s.stats.Misses
	if totalRequests == 0 {
		return 0.0
	}

	return float64(s.stats.Hits) / float64(totalRequests)
}
