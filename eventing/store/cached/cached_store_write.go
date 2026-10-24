package cached

import (
	"context"
	"strings"

	"gochen/errors"
	"gochen/eventing"
)

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
