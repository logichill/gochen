package cached

import (
	"container/list"
	"context"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gochen/clock"
	"gochen/contextx"
	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/monitoring"
	"gochen/eventing/store"
)

// CachedEventStore 带缓存的事件存储装饰器。
// 使用缓存提升事件读取性能，适合读多写少的场景。
type CachedEventStore[ID comparable] struct {
	store    store.IEventStreamStore[ID] // 底层事件存储
	cache    *EventCache[ID]             // 缓存层
	clock    clock.IClock                // 时间来源
	stats    *CacheStats                 // 缓存统计
	metrics  atomic.Value                // cacheMetricsHolder（承载 monitoring.ICacheMetricsRecorder），用于并发热替换且避免 data race
	interval time.Duration

	stopCh chan struct{}
	// 关闭标识用于避免重复关闭
	stopOnce  sync.Once
	startOnce sync.Once
}

var _ store.IEventStreamStore[int64] = (*CachedEventStore[int64])(nil)

type cacheMetricsHolder struct {
	rec monitoring.ICacheMetricsRecorder
}

// NewCachedEventStore 创建Cached事件存储。
func NewCachedEventStore[ID comparable](inner store.IEventStreamStore[ID], config *Config) *CachedEventStore[ID] {
	if config == nil {
		config = DefaultConfig()
	} else {
		configCopy := *config
		config = &configCopy
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
	if isNilCachedDependency(config.Clock) {
		config.Clock = clock.NewRealClock()
	}

	cache := &EventCache[ID]{
		aggregateCache: make(map[aggregateCacheKey[ID]]*CachedAggregate[ID]),
		lru:            list.New(),
		lruIndex:       make(map[aggregateCacheKey[ID]]*list.Element),
		generations:    make(map[aggregateCacheKey[ID]]uint64),
		activeFills:    make(map[aggregateCacheKey[ID]]int),
		ttl:            config.TTL,
		maxAggregates:  config.MaxAggregates,
	}

	cached := &CachedEventStore[ID]{
		store:    inner,
		cache:    cache,
		clock:    config.Clock,
		stats:    &CacheStats{},
		interval: config.CleanupInterval,
		stopCh:   make(chan struct{}),
	}

	if !config.DisableCleanup {
		cached.StartCleanupWorker()
	}

	return cached
}

// StartCleanupWorker 显式启动后台清理协程。
func (s *CachedEventStore[ID]) StartCleanupWorker() {
	if s == nil {
		return
	}
	s.startOnce.Do(func() {
		go s.startCleanupWorker(s.interval)
	})
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

// AppendEvents 保存事件并失效缓存。
func (s *CachedEventStore[ID]) AppendEvents(ctx context.Context, aggregateType string, aggregateID ID, events []eventing.IStorableEvent[ID], expectedVersion uint64) error {
	if err := cachedStoreContextError(ctx); err != nil {
		return err
	}
	if err := s.validateCacheStore(); err != nil {
		return err
	}
	aggregateType = strings.TrimSpace(aggregateType)
	if aggregateType == "" {
		return errors.NewCode(errors.InvalidInput, "aggregate type cannot be empty")
	}
	// 写入底层存储
	if err := s.store.AppendEvents(ctx, aggregateType, aggregateID, events, expectedVersion); err != nil {
		return err
	}

	// typed EventStore 以显式 aggregateType 为聚合身份的一部分。
	s.invalidateCache(aggregateType, aggregateID)
	return nil
}

// LoadEvents 加载指定聚合类型的事件（优先从缓存）。
func (s *CachedEventStore[ID]) LoadEvents(ctx context.Context, aggregateType string, aggregateID ID, afterVersion uint64) ([]eventing.Event[ID], error) {
	if err := cachedStoreContextError(ctx); err != nil {
		return nil, err
	}
	if err := s.validateCacheStore(); err != nil {
		return nil, err
	}
	aggregateType = strings.TrimSpace(aggregateType)
	if aggregateType == "" {
		return nil, errors.NewCode(errors.InvalidInput, "aggregate type cannot be empty")
	}
	key := cacheKey(aggregateType, aggregateID)
	key.tenantID = contextx.TenantID(ctx)
	if cached := s.getCachedEvents(key, afterVersion); cached != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		s.recordHit()
		return cached, nil
	}

	s.recordMiss()

	var (
		events []eventing.Event[ID]
		err    error
	)
	var generation uint64
	if afterVersion == 0 {
		generation = s.beginCacheFill(key)
	}
	events, err = s.store.LoadEvents(ctx, aggregateType, aggregateID, afterVersion)

	if err != nil {
		if afterVersion == 0 {
			s.finishCacheFill(key, nil, generation)
		}
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		if afterVersion == 0 {
			s.finishCacheFill(key, nil, generation)
		}
		return nil, err
	}

	if afterVersion == 0 {
		s.finishCacheFill(key, events, generation)
	}

	return cloneEvents(events), nil
}

// StreamEvents 基于游标/类型过滤读取事件流（如底层支持则委托，否则返回能力缺失错误）。
func (s *CachedEventStore[ID]) StreamEvents(ctx context.Context, opts *store.StreamOptions) (*store.StreamResult[ID], error) {
	if err := cachedStoreContextError(ctx); err != nil {
		return nil, err
	}
	if err := s.validateInnerStore(); err != nil {
		return nil, err
	}
	return s.store.StreamEvents(ctx, opts)
}

// StreamAggregate 按聚合顺序流式读取事件（委托到底层存储）。
func (s *CachedEventStore[ID]) StreamAggregate(ctx context.Context, opts *store.AggregateStreamOptions[ID]) (*store.AggregateStreamResult[ID], error) {
	if err := cachedStoreContextError(ctx); err != nil {
		return nil, err
	}
	if err := s.validateInnerStore(); err != nil {
		return nil, err
	}
	return s.store.StreamAggregate(ctx, opts)
}

// HasAggregate 检查聚合是否存在。
func (s *CachedEventStore[ID]) HasAggregate(ctx context.Context, aggregateType string, aggregateID ID) (bool, error) {
	if err := cachedStoreContextError(ctx); err != nil {
		return false, err
	}
	if err := s.validateInnerStore(); err != nil {
		return false, err
	}
	return s.store.HasAggregate(ctx, aggregateType, aggregateID)
}

func (s *CachedEventStore[ID]) GetAggregateVersion(ctx context.Context, aggregateType string, aggregateID ID) (uint64, error) {
	if err := cachedStoreContextError(ctx); err != nil {
		return 0, err
	}
	if err := s.validateInnerStore(); err != nil {
		return 0, err
	}
	return s.store.GetAggregateVersion(ctx, aggregateType, aggregateID)
}

func (s *CachedEventStore[ID]) validateInnerStore() error {
	if s == nil {
		return errors.NewCode(errors.InvalidInput, "cached event store is nil")
	}
	if isNilCachedDependency(s.store) {
		return errors.NewCode(errors.InvalidInput, "inner event store cannot be nil")
	}
	return nil
}

func (s *CachedEventStore[ID]) validateCacheStore() error {
	if err := s.validateInnerStore(); err != nil {
		return err
	}
	if s.cache == nil {
		return errors.NewCode(errors.InvalidInput, "cached event store cache is not initialized")
	}
	return nil
}

func cachedStoreContextError(ctx context.Context) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	return ctx.Err()
}

func isNilCachedDependency(value any) bool {
	if value == nil {
		return true
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return rv.IsNil()
	default:
		return false
	}
}
