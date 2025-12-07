package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"gochen/eventing"
)

// MemoryEventStore 一个简单的内存实现，仅用于测试与示例
//
// 当前内置实现仍以 int64 作为聚合 ID 类型，对应 IEventStore[int64]。
// 如需自定义 ID 类型，可单独实现基于 IEventStore[ID] 的内存版本。
type MemoryEventStore struct {
	mu     sync.RWMutex
	events map[string][]eventing.Event[int64] // aggregateType:aggregateID -> ordered events
}

func NewMemoryEventStore() *MemoryEventStore {
	return &MemoryEventStore{events: make(map[string][]eventing.Event[int64])}
}

func (m *MemoryEventStore) AppendEvents(ctx context.Context, aggregateID int64, events []eventing.IStorableEvent[int64], expectedVersion uint64) error {
	if len(events) == 0 {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	aggregateType := ""
	aggregateType = events[0].GetAggregateType()
	key := eventAggregateKey(aggregateType, aggregateID)
	currentVersion, err := m.getAggregateVersionUnsafe(key)
	if err != nil {
		return err
	}
	if currentVersion != expectedVersion {
		return eventing.NewConcurrencyError(aggregateID, expectedVersion, currentVersion)
	}
	for i, e := range events {
		if err := e.Validate(); err != nil {
			return err
		}
		expectedEventVersion := expectedVersion + uint64(i) + 1
		if e.GetVersion() != expectedEventVersion {
			return fmt.Errorf("event version not sequential: expected %d, got %d", expectedEventVersion, e.GetVersion())
		}
	}
	if m.events[key] == nil {
		m.events[key] = make([]eventing.Event[int64], 0, len(events))
	}
	// basic validation and append in order
	for _, e := range events {
		// 安全类型转换：从 IStorableEvent 到 Event
		event, ok := e.(*eventing.Event[int64])
		if !ok {
			return fmt.Errorf("unsupported event type: %T, expected *eventing.Event", e)
		}
		m.events[key] = append(m.events[key], *event)
	}
	return nil
}

func (m *MemoryEventStore) LoadEvents(ctx context.Context, aggregateID int64, afterVersion uint64) ([]eventing.Event[int64], error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	// 使用精确的后缀匹配：":aggregateID"
	// 确保不会误匹配，例如 ":123" 不会匹配 ":1234"
	suffix := fmt.Sprintf(":%d", aggregateID)
	var aggregateEvents []eventing.Event[int64]
	for key, evts := range m.events {
		// 检查 key 是否以 suffix 结尾，且 suffix 前面是 aggregateType（不含冒号）
		// key 格式为 "aggregateType:aggregateID"
		if strings.HasSuffix(key, suffix) && (len(key) == len(suffix) || key[len(key)-len(suffix)-1] != ':') {
			aggregateEvents = append(aggregateEvents, evts...)
		}
	}
	if len(aggregateEvents) == 0 {
		return []eventing.Event[int64]{}, nil
	}
	sort.SliceStable(aggregateEvents, func(i, j int) bool { return aggregateEvents[i].GetVersion() < aggregateEvents[j].GetVersion() })
	res := make([]eventing.Event[int64], 0, len(aggregateEvents))
	for _, e := range aggregateEvents {
		if e.GetVersion() > afterVersion {
			res = append(res, e)
		}
	}
	return res, nil
}

// LoadEventsByType 加载指定聚合类型的事件（可选接口）
func (m *MemoryEventStore) LoadEventsByType(ctx context.Context, aggregateType string, aggregateID int64, afterVersion uint64) ([]eventing.Event[int64], error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	key := eventAggregateKey(aggregateType, aggregateID)
	aggregateEvents := m.events[key]
	if len(aggregateEvents) == 0 {
		return []eventing.Event[int64]{}, nil
	}
	res := make([]eventing.Event[int64], 0, len(aggregateEvents))
	for _, e := range aggregateEvents {
		if e.GetVersion() > afterVersion {
			res = append(res, e)
		}
	}
	return res, nil
}

// StreamAggregate 按聚合顺序流式读取事件（实现 IAggregateEventStore[int64]，可选能力）
func (m *MemoryEventStore) StreamAggregate(ctx context.Context, opts *AggregateStreamOptions[int64]) (*AggregateStreamResult[int64], error) {
	if opts == nil {
		return nil, fmt.Errorf("AggregateStreamOptions cannot be nil")
	}
	if opts.AggregateID <= 0 {
		return nil, fmt.Errorf("invalid aggregate id %d", opts.AggregateID)
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	key := eventAggregateKey(opts.AggregateType, opts.AggregateID)
	aggregateEvents := m.events[key]
	if len(aggregateEvents) == 0 {
		return &AggregateStreamResult[int64]{Events: []eventing.Event[int64]{}}, nil
	}

	res := make([]eventing.Event[int64], 0, limit)
	var nextVersion uint64
	for _, e := range aggregateEvents {
		if e.GetVersion() <= opts.AfterVersion {
			continue
		}
		res = append(res, e)
		nextVersion = e.GetVersion()
		if len(res) >= limit {
			break
		}
	}

	result := &AggregateStreamResult[int64]{Events: res, NextVersion: nextVersion}
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

func (m *MemoryEventStore) StreamEvents(ctx context.Context, from time.Time) ([]eventing.Event[int64], error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var res []eventing.Event[int64]
	for _, arr := range m.events {
		for _, e := range arr {
			if !from.IsZero() && e.GetTimestamp().Before(from) {
				continue
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
	return res, nil
}

// GetEventStreamWithCursor 基于游标/类型过滤读取事件流（实现 IEventStoreExtended[int64]）
func (m *MemoryEventStore) GetEventStreamWithCursor(ctx context.Context, opts *StreamOptions) (*StreamResult[int64], error) {
	m.mu.RLock()
	var all []eventing.Event[int64]
	for _, arr := range m.events {
		all = append(all, arr...)
	}
	m.mu.RUnlock()

	result := FilterEventsWithOptions[int64](all, opts)
	return result, nil
}

// HasAggregate 检查聚合是否存在
func (m *MemoryEventStore) HasAggregate(ctx context.Context, aggregateID int64) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	suffix := fmt.Sprintf(":%d", aggregateID)
	for key, events := range m.events {
		if strings.HasSuffix(key, suffix) && len(events) > 0 {
			return true, nil
		}
	}
	return false, nil
}

// GetAggregateVersion 返回聚合当前版本
func (m *MemoryEventStore) GetAggregateVersion(ctx context.Context, aggregateID int64) (uint64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	suffix := fmt.Sprintf(":%d", aggregateID)
	var latest uint64
	for key, events := range m.events {
		if strings.HasSuffix(key, suffix) && len(events) > 0 {
			if v := events[len(events)-1].GetVersion(); v > latest {
				latest = v
			}
		}
	}
	return latest, nil
}

func eventAggregateKey(aggregateType string, aggregateID int64) string {
	return fmt.Sprintf("%s:%d", aggregateType, aggregateID)
}

func (m *MemoryEventStore) getAggregateVersionUnsafe(key string) (uint64, error) {
	aggregateEvents, exists := m.events[key]
	if !exists || len(aggregateEvents) == 0 {
		return 0, nil
	}
	return aggregateEvents[len(aggregateEvents)-1].GetVersion(), nil
}

// 确认实现拓展接口
var _ IEventStoreExtended[int64] = (*MemoryEventStore)(nil)
