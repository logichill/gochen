package store

import (
	"context"
	"sort"
	"strings"

	"gochen/errors"
	"gochen/eventing"
)

// LoadEvents 返回指定聚合类型下、某个版本之后的事件。
func (m *MemoryEventStore[ID]) LoadEvents(ctx context.Context, aggregateType string, aggregateID ID, afterVersion uint64) ([]eventing.Event[ID], error) {
	if err := memoryEventStoreContextError(ctx); err != nil {
		return nil, err
	}
	if m == nil {
		return nil, errors.NewCode(errors.InvalidInput, "memory event store is nil")
	}
	aggregateType = strings.TrimSpace(aggregateType)
	if aggregateType == "" {
		return nil, errors.NewCode(errors.InvalidInput, "aggregate type cannot be empty")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key := memoryAggregateKey[ID]{aggregateType: aggregateType, aggregateID: aggregateID}
	aggregateEvents := m.events[key]
	if len(aggregateEvents) == 0 {
		return []eventing.Event[ID]{}, nil
	}
	startIdx := searchMemoryEventsAfterVersion(aggregateEvents, afterVersion)
	return cloneMemoryEvents(aggregateEvents[startIdx:]), nil
}

// StreamAggregate 按版本顺序分页读取单个聚合的事件流。
func (m *MemoryEventStore[ID]) StreamAggregate(ctx context.Context, opts *AggregateStreamOptions[ID]) (*AggregateStreamResult[ID], error) {
	if err := memoryEventStoreContextError(ctx); err != nil {
		return nil, err
	}
	if m == nil {
		return nil, errors.NewCode(errors.InvalidInput, "memory event store is nil")
	}
	if opts == nil {
		return nil, errors.NewCode(errors.InvalidInput, "AggregateStreamOptions cannot be nil")
	}
	if invalidMemoryAggregateID(opts.AggregateID) {
		return nil, errors.NewCode(errors.InvalidInput, "invalid aggregate id").WithContext("aggregate_id", opts.AggregateID)
	}
	aggregateType := strings.TrimSpace(opts.AggregateType)
	if aggregateType == "" {
		return nil, errors.NewCode(errors.InvalidInput, "aggregate type cannot be empty")
	}
	limit := NormalizeStreamLimit(opts.Limit)

	m.mu.RLock()
	defer m.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	key := memoryAggregateKey[ID]{aggregateType: aggregateType, aggregateID: opts.AggregateID}
	aggregateEvents := m.events[key]
	if len(aggregateEvents) == 0 {
		return &AggregateStreamResult[ID]{Events: []eventing.Event[ID]{}}, nil
	}

	res := make([]eventing.Event[ID], 0, limit)
	var nextVersion uint64
	for _, e := range aggregateEvents {
		if e.GetVersion() <= opts.AfterVersion {
			continue
		}
		res = append(res, cloneMemoryEvent(e))
		nextVersion = e.GetVersion()
		if len(res) >= limit {
			break
		}
	}

	result := &AggregateStreamResult[ID]{Events: res, NextVersion: nextVersion}
	// HasMore：若还有事件版本大于 nextVersion，则标记为 true
	if nextVersion > 0 {
		for _, e := range aggregateEvents {
			if e.GetVersion() > nextVersion {
				result.HasMore = true
				break
			}
		}
	}
	return result, nil
}

// StreamEvents 对全局事件集应用过滤和游标分页。
func (m *MemoryEventStore[ID]) StreamEvents(ctx context.Context, opts *StreamOptions) (*StreamResult[ID], error) {
	if err := memoryEventStoreContextError(ctx); err != nil {
		return nil, err
	}
	if m == nil {
		return nil, errors.NewCode(errors.InvalidInput, "memory event store is nil")
	}
	m.mu.RLock()
	if err := ctx.Err(); err != nil {
		m.mu.RUnlock()
		return nil, err
	}
	if opts != nil && opts.After != "" {
		if _, exists := m.eventByID[opts.After]; !exists {
			m.mu.RUnlock()
			return nil, errors.NewCode(errors.NotFound, "cursor not found").WithContext("cursor", opts.After)
		}
	}
	all := append([]eventing.Event[ID](nil), m.globalEvents...)
	m.mu.RUnlock()

	result := FilterOrderedEventsWithOptions[ID](all, opts)
	result.Events = cloneMemoryEvents(result.Events)
	return result, nil
}

// HasAggregate 通过是否已存在事件流判断聚合是否存在。
func (m *MemoryEventStore[ID]) HasAggregate(ctx context.Context, aggregateType string, aggregateID ID) (bool, error) {
	version, err := m.GetAggregateVersion(ctx, aggregateType, aggregateID)
	return version > 0, err
}

// GetAggregateVersion 返回指定聚合类型和 ID 的当前版本号。
func (m *MemoryEventStore[ID]) GetAggregateVersion(ctx context.Context, aggregateType string, aggregateID ID) (uint64, error) {
	if err := memoryEventStoreContextError(ctx); err != nil {
		return 0, err
	}
	if m == nil {
		return 0, errors.NewCode(errors.InvalidInput, "memory event store is nil")
	}
	aggregateType = strings.TrimSpace(aggregateType)
	if aggregateType == "" {
		return 0, errors.NewCode(errors.InvalidInput, "aggregate type cannot be empty")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return m.getAggregateVersionUnsafe(memoryAggregateKey[ID]{aggregateType: aggregateType, aggregateID: aggregateID})
}

func searchMemoryEventsAfterVersion[ID comparable](events []eventing.Event[ID], afterVersion uint64) int {
	return sort.Search(len(events), func(i int) bool {
		return events[i].GetVersion() > afterVersion
	})
}
