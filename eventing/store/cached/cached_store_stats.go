package cached

import (
	"gochen/eventing/monitoring"
)

func (s *CachedEventStore[ID]) CacheStats() monitoring.CacheStats {
	if s == nil || s.stats == nil {
		return monitoring.CacheStats{}
	}

	s.stats.mutex.RLock()
	hits := s.stats.Hits
	misses := s.stats.Misses
	evictions := s.stats.Evictions
	invalidations := s.stats.Invalidations
	s.stats.mutex.RUnlock()

	cacheSize, maxSize := 0, 0
	ttlSeconds := 0.0
	if s.cache != nil {
		s.cache.mutex.RLock()
		cacheSize = len(s.cache.aggregateCache)
		maxSize = s.cache.maxAggregates
		ttlSeconds = s.cache.ttl.Seconds()
		s.cache.mutex.RUnlock()
	}

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
	if s == nil || s.stats == nil {
		return
	}
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
	if s == nil || s.stats == nil {
		return
	}
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
	s.recordEvictions(1)
}

// recordEvictions 批量记录缓存驱逐，避免逐条调用外部指标记录器。
//
// 调用方必须已释放 s.cache.mutex：外部记录器不受本包控制，持锁调用有死锁风险。
func (s *CachedEventStore[ID]) recordEvictions(count int) {
	if s == nil || s.stats == nil || count <= 0 {
		return
	}
	var m monitoring.ICacheMetricsRecorder
	s.stats.mutex.Lock()
	s.stats.Evictions += int64(count)
	m = s.getMetrics()
	s.stats.mutex.Unlock()

	if m != nil {
		for i := 0; i < count; i++ {
			m.RecordCacheEviction()
		}
	}
}

// recordInvalidation 记录缓存失效。
func (s *CachedEventStore[ID]) recordInvalidation() {
	if s == nil || s.stats == nil {
		return
	}
	s.stats.mutex.Lock()
	defer s.stats.mutex.Unlock()
	s.stats.Invalidations++
}

// Stats 从存储中查询数据。
//
// 说明：
// - Stats 获取缓存统计信息（简化版）
func (s *CachedEventStore[ID]) Stats() *CacheStats {
	if s == nil || s.stats == nil {
		return &CacheStats{}
	}
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
	if s == nil || s.stats == nil {
		return 0
	}
	s.stats.mutex.RLock()
	defer s.stats.mutex.RUnlock()

	totalRequests := s.stats.Hits + s.stats.Misses
	if totalRequests == 0 {
		return 0.0
	}

	return float64(s.stats.Hits) / float64(totalRequests)
}

type cacheMetricsHolder struct {
	rec monitoring.ICacheMetricsRecorder
}

// SetMetricsRecorder 设置缓存指标记录器（可选）。
//
// 并发语义：允许在运行时注入/替换 recorder（内部使用 atomic.Value），避免与 Record* 并发访问时产生 data race。
func (s *CachedEventStore[ID]) SetMetricsRecorder(rec monitoring.ICacheMetricsRecorder) {
	if s == nil {
		return
	}
	// atomic.Value 不允许 Store(nil)，且要求后续 Store 的动态类型一致。
	// 用稳定的 holder 类型承载 interface，可以安全地设置 nil/不同实现。
	s.metrics.Store(cacheMetricsHolder{rec: rec})
}

func (s *CachedEventStore[ID]) getMetrics() monitoring.ICacheMetricsRecorder {
	if s == nil {
		return nil
	}
	v := s.metrics.Load()
	if v == nil {
		return nil
	}
	h, ok := v.(cacheMetricsHolder)
	if !ok {
		return nil
	}
	if isNilCachedDependency(h.rec) {
		return nil
	}
	return h.rec
}
