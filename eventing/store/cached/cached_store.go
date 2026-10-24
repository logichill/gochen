package cached

import (
	"container/list"
	"context"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"gochen/clock"
	"gochen/errors"
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
	// cleanupWG 追踪后台清理协程，Close 时等待其退出，避免优雅停机
	// 后仍有未决协程在访问已关闭的缓存/底层存储。
	cleanupWG sync.WaitGroup
}

var _ store.IEventStreamStore[int64] = (*CachedEventStore[int64])(nil)

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
