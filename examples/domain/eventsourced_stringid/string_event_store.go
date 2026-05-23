package main

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"sync"

	"gochen/eventing"
	estore "gochen/eventing/store"
)

// StringMemoryEventStore 是一个基于字符串聚合 ID 的内存事件存储实现。
//
// 设计目标：
//   - 仅用于示例与测试，展示如何在业务仓库实现 IEventStore[ID] 接口；
//   - 通过在示例内部实现 IEventStore[string]，演示与 DomainEventStore[T, string] 的组合方式。
type StringMemoryEventStore struct {
	mu     sync.RWMutex
	events map[string][]eventing.Event[string] // key: aggregateID
}

// NewStringMemoryEventStore 创建一个基于 string 聚合 ID 的内存事件存储。
func NewStringMemoryEventStore() *StringMemoryEventStore {
	return &StringMemoryEventStore{
		events: make(map[string][]eventing.Event[string]),
	}
}

// AppendEvents 以乐观锁方式向指定聚合的事件流追加事件。
func (m *StringMemoryEventStore) AppendEvents(ctx context.Context, aggregateID string, events []eventing.IStorableEvent[string], expectedVersion uint64) error {
	if len(events) == 0 {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	currentEvents := m.events[aggregateID]
	var currentVersion uint64
	if len(currentEvents) > 0 {
		currentVersion = currentEvents[len(currentEvents)-1].GetVersion()
	}

	if currentVersion != expectedVersion {
		return fmt.Errorf("concurrency conflict for aggregate %s: expected version %d, actual version %d", aggregateID, expectedVersion, currentVersion)
	}

	for i, e := range events {
		if err := e.Validate(); err != nil {
			return err
		}

		expectedEventVersion := expectedVersion + uint64(i) + 1
		if e.GetVersion() != expectedEventVersion {
			return fmt.Errorf("event version not sequential: expected %d, got %d", expectedEventVersion, e.GetVersion())
		}

		ev, ok := e.(*eventing.Event[string])
		if !ok {
			return fmt.Errorf("unsupported event type: %T, expected *eventing.Event[string]", e)
		}
		currentEvents = append(currentEvents, *ev)
	}

	m.events[aggregateID] = currentEvents
	return nil
}

// LoadEvents 返回指定版本之后的聚合事件。
func (m *StringMemoryEventStore) LoadEvents(ctx context.Context, aggregateID string, afterVersion uint64) ([]eventing.Event[string], error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	all := m.events[aggregateID]
	if len(all) == 0 {
		return []eventing.Event[string]{}, nil
	}

	res := make([]eventing.Event[string], 0, len(all))
	for _, e := range all {
		if e.GetVersion() > afterVersion {
			res = append(res, e)
		}
	}
	return res, nil
}

// LoadEventsByType 在读取事件后额外按聚合类型做一次过滤。
func (m *StringMemoryEventStore) LoadEventsByType(ctx context.Context, aggregateType string, aggregateID string, afterVersion uint64) ([]eventing.Event[string], error) {
	events, err := m.LoadEvents(ctx, aggregateID, afterVersion)
	if err != nil {
		return nil, err
	}
	if aggregateType == "" {
		return events, nil
	}
	result := make([]eventing.Event[string], 0, len(events))
	for _, e := range events {
		if e.AggregateType == aggregateType {
			result = append(result, e)
		}
	}
	return result, nil
}

// HasAggregate 通过是否已有事件判断聚合是否存在。
func (m *StringMemoryEventStore) HasAggregate(ctx context.Context, aggregateID string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	events := m.events[aggregateID]
	return len(events) > 0, nil
}

// GetAggregateVersion 返回当前聚合事件流的最新版本号。
func (m *StringMemoryEventStore) GetAggregateVersion(ctx context.Context, aggregateID string) (uint64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	events := m.events[aggregateID]
	if len(events) == 0 {
		return 0, nil
	}
	return events[len(events)-1].GetVersion(), nil
}

// StreamEvents 用简单 offset 游标分页读取全局事件流。
func (m *StringMemoryEventStore) StreamEvents(ctx context.Context, opts *estore.StreamOptions) (*estore.StreamResult[string], error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	limit := estore.DefaultStreamLimit
	if opts != nil && opts.Limit > 0 {
		limit = opts.Limit
	}

	start := 0
	if opts != nil && opts.After != "" {
		if n, err := strconv.Atoi(opts.After); err == nil && n >= 0 {
			start = n
		}
	}

	var res []eventing.Event[string]
	for _, arr := range m.events {
		for _, e := range arr {
			if opts != nil {
				if !opts.FromTime.IsZero() && e.GetTimestamp().Before(opts.FromTime) {
					continue
				}
				if !opts.ToTime.IsZero() && e.GetTimestamp().After(opts.ToTime) {
					continue
				}
				if len(opts.Types) > 0 {
					ok := false
					for _, t := range opts.Types {
						if t == e.GetType() {
							ok = true
							break
						}
					}
					if !ok {
						continue
					}
				}
				if len(opts.AggregateTypes) > 0 {
					ok := false
					for _, t := range opts.AggregateTypes {
						if t == e.GetAggregateType() {
							ok = true
							break
						}
					}
					if !ok {
						continue
					}
				}
			}
			res = append(res, e)
		}
	}

	sort.Slice(res, func(i, j int) bool {
		if res[i].GetTimestamp().Equal(res[j].GetTimestamp()) {
			return res[i].GetID() < res[j].GetID()
		}
		return res[i].GetTimestamp().Before(res[j].GetTimestamp())
	})

	if start >= len(res) {
		return &estore.StreamResult[string]{Events: []eventing.Event[string]{}, NextCursor: "", HasMore: false}, nil
	}
	end := start + limit
	if end > len(res) {
		end = len(res)
	}
	page := res[start:end]
	hasMore := end < len(res)
	nextCursor := ""
	if hasMore {
		nextCursor = strconv.Itoa(end)
	}

	return &estore.StreamResult[string]{
		Events:     page,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	}, nil
}

// StreamAggregate 按版本顺序分页读取单个聚合的事件流。
func (m *StringMemoryEventStore) StreamAggregate(ctx context.Context, opts *estore.AggregateStreamOptions[string]) (*estore.AggregateStreamResult[string], error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if opts == nil {
		return &estore.AggregateStreamResult[string]{Events: []eventing.Event[string]{}, NextVersion: 0, HasMore: false}, nil
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = estore.DefaultStreamLimit
	}

	all := m.events[opts.AggregateID]
	candidates := make([]eventing.Event[string], 0, len(all))
	for _, e := range all {
		if opts.AggregateType != "" && e.AggregateType != opts.AggregateType {
			continue
		}
		if e.GetVersion() > opts.AfterVersion {
			candidates = append(candidates, e)
		}
	}

	if len(candidates) == 0 {
		return &estore.AggregateStreamResult[string]{Events: []eventing.Event[string]{}, NextVersion: opts.AfterVersion, HasMore: false}, nil
	}

	end := limit
	if end > len(candidates) {
		end = len(candidates)
	}
	page := candidates[:end]
	last := page[len(page)-1].GetVersion()
	hasMore := end < len(candidates)

	return &estore.AggregateStreamResult[string]{
		Events:      page,
		NextVersion: last,
		HasMore:     hasMore,
	}, nil
}

// 编译期断言：确保实现了 IEventStreamStore[string] 接口。
var _ estore.IEventStreamStore[string] = (*StringMemoryEventStore)(nil)
