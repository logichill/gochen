package cached

import (
	"context"
	"strings"

	"gochen/contextx"
	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/store"
)

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
