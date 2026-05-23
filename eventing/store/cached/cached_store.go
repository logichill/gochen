package cached

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"gochen/eventing"
	"gochen/eventing/monitoring"
	"gochen/eventing/store"
)

// CachedEventStore 带缓存的事件存储装饰器。
// 使用缓存提升事件读取性能，适合读多写少的场景。
type CachedEventStore[ID comparable] struct {
	store   store.IEventStreamStore[ID] // 底层事件存储
	cache   *EventCache[ID]             // 缓存层
	stats   *CacheStats                 // 缓存统计
	metrics atomic.Value                // cacheMetricsHolder（承载 monitoring.ICacheMetricsRecorder），用于并发热替换且避免 data race

	stopCh chan struct{}
	// 关闭标识用于避免重复关闭
	stopOnce sync.Once
}

type cacheMetricsHolder struct {
	rec monitoring.ICacheMetricsRecorder
}

// NewCachedEventStore 创建Cached事件存储。
func NewCachedEventStore[ID comparable](inner store.IEventStreamStore[ID], config *Config) *CachedEventStore[ID] {
	if config == nil {
		config = DefaultConfig()
	}

	// 验证并修正配置值
	if config.CleanupInterval <= 0 {
		config.CleanupInterval = 1 * time.Minute
	}
	if config.TTL <= 0 {
		config.TTL = 5 * time.Minute
	}
	if config.MaxAggregates <= 0 {
		config.MaxAggregates = defaultMaxAggregates
	}

	cache := &EventCache[ID]{
		aggregateCache: make(map[string]*CachedAggregate[ID]),
		ttl:            config.TTL,
		maxAggregates:  config.MaxAggregates,
	}

	cached := &CachedEventStore[ID]{
		store:  inner,
		cache:  cache,
		stats:  &CacheStats{},
		stopCh: make(chan struct{}),
	}

	// 启动定期清理过期缓存
	go cached.startCleanupWorker(config.CleanupInterval)

	return cached
}

// SetMetricsRecorder 设置缓存指标记录器（可选）。
//
// 并发语义：允许在运行时注入/替换 recorder（内部使用 atomic.Value），避免与 Record* 并发访问时产生 data race。
func (s *CachedEventStore[ID]) SetMetricsRecorder(rec monitoring.ICacheMetricsRecorder) {
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
	return h.rec
}

// AppendEvents 保存事件并失效缓存。
func (s *CachedEventStore[ID]) AppendEvents(ctx context.Context, aggregateID ID, events []eventing.IStorableEvent[ID], expectedVersion uint64) error {
	// 写入底层存储
	if err := s.store.AppendEvents(ctx, aggregateID, events, expectedVersion); err != nil {
		return err
	}

	// 失效缓存：清理按类型缓存的条目和通用条目
	if len(events) > 0 {
		aggregateType := events[0].GetAggregateType()
		if aggregateType != "" {
			s.invalidateCache(aggregateType, aggregateID)
		}
	}
	// 始终清理通用缓存（key 为空类型）
	s.invalidateCache("", aggregateID)

	return nil
}

// LoadEvents 加载聚合事件。
//
// 说明：
// - LoadEvents 加载聚合的事件（优先从缓存）
func (s *CachedEventStore[ID]) LoadEvents(ctx context.Context, aggregateID ID, afterVersion uint64) ([]eventing.Event[ID], error) {
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

// LoadEventsByType 加载聚合事件（按聚合类型）。
//
// 说明：
// - LoadEventsByType 加载指定聚合类型的事件（优先从缓存）
func (s *CachedEventStore[ID]) LoadEventsByType(ctx context.Context, aggregateType string, aggregateID ID, afterVersion uint64) ([]eventing.Event[ID], error) {
	key := cacheKey(aggregateType, aggregateID)
	if cached := s.getCachedEvents(key, afterVersion); cached != nil {
		s.recordHit()
		return cached, nil
	}

	s.recordMiss()

	var (
		events []eventing.Event[ID]
		err    error
	)
	events, err = s.store.LoadEventsByType(ctx, aggregateType, aggregateID, afterVersion)

	if err != nil {
		return nil, err
	}

	if afterVersion == 0 && len(events) > 0 {
		s.cacheAggregate(key, events)
	}

	return events, nil
}

// StreamEvents 基于游标/类型过滤读取事件流（如底层支持则委托，否则返回能力缺失错误）。
func (s *CachedEventStore[ID]) StreamEvents(ctx context.Context, opts *store.StreamOptions) (*store.StreamResult[ID], error) {
	return s.store.StreamEvents(ctx, opts)
}

// StreamAggregate 按聚合顺序流式读取事件（委托到底层存储）。
func (s *CachedEventStore[ID]) StreamAggregate(ctx context.Context, opts *store.AggregateStreamOptions[ID]) (*store.AggregateStreamResult[ID], error) {
	return s.store.StreamAggregate(ctx, opts)
}

// HasAggregate 检查聚合是否存在。
func (s *CachedEventStore[ID]) HasAggregate(ctx context.Context, aggregateID ID) (bool, error) {
	return s.store.HasAggregate(ctx, aggregateID)
}

func (s *CachedEventStore[ID]) GetAggregateVersion(ctx context.Context, aggregateID ID) (uint64, error) {
	return s.store.GetAggregateVersion(ctx, aggregateID)
}
