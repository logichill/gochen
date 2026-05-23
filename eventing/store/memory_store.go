package store

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"gochen/errors"
	"gochen/eventing"
)

// MemoryEventStore 一个简单的内存实现，仅用于测试与示例。
//
// 当前内置实现仍以 int64 作为聚合 ID 类型，对应 IEventStore[int64] 与 IEventStreamStore[int64]。
// 如需自定义 ID 类型，可单独实现基于 IEventStore[ID] 的内存版本。
type MemoryEventStore struct {
	mu sync.RWMutex
	// events 按 aggregateType:aggregateID 维度组织，保留原有语义用于按类型查询与游标扫描。
	events map[string][]eventing.Event[int64] // aggregateType:aggregateID -> ordered events
	// eventsByID 按聚合 ID 维度组织，用于快速按聚合 ID 读取/检查版本，避免对 events 做 O(N) 级扫描。
	eventsByID map[int64][]eventing.Event[int64] // aggregateID -> ordered events (跨类型聚合)
}

// NewMemoryEventStore 创建一个仅供测试和示例使用的内存事件存储。
func NewMemoryEventStore() *MemoryEventStore {
	return &MemoryEventStore{
		events:     make(map[string][]eventing.Event[int64]),
		eventsByID: make(map[int64][]eventing.Event[int64]),
	}
}

// NewMemoryAggregateStore 以 `IEventStore[int64]` 视图返回内存事件存储。
func NewMemoryAggregateStore() IEventStore[int64] {
	return NewMemoryEventStore()
}

// NewMemoryEventStreamStore 以 `IEventStreamStore[int64]` 视图返回内存事件存储。
func NewMemoryEventStreamStore() IEventStreamStore[int64] {
	return NewMemoryEventStore()
}

// AppendEvents 以乐观锁语义向指定聚合追加一批事件。
func (m *MemoryEventStore) AppendEvents(ctx context.Context, aggregateID int64, events []eventing.IStorableEvent[int64], expectedVersion uint64) error {
	if len(events) == 0 {
		return nil
	}

	// 性能优化：预先在锁外进行验证和类型转换，减少临界区范围
	// 这些操作不依赖共享状态，可以安全地在锁外执行
	aggregateType := events[0].GetAggregateType()
	convertedEvents := make([]eventing.Event[int64], len(events))

	for i, e := range events {
		// 验证事件（锁外）
		if err := e.Validate(); err != nil {
			return err
		}

		// 类型转换（锁外）
		event, ok := e.(*eventing.Event[int64])
		if !ok {
			return errors.NewCode(errors.InvalidInput, fmt.Sprintf("unsupported event type: %T, expected *eventing.Event", e))
		}

		// 版本校验（锁外）
		expectedEventVersion := expectedVersion + uint64(i) + 1
		if event.GetVersion() != expectedEventVersion {
			return errors.NewCode(errors.InvalidInput, fmt.Sprintf("event version not sequential: expected %d, got %d", expectedEventVersion, event.GetVersion()))
		}

		convertedEvents[i] = *event
	}

	// 性能优化：缩小锁范围，只在真正修改共享状态时持锁
	// 锁持有时间从 O(n*验证时间) 降低到 O(n*append操作)
	m.mu.Lock()
	defer m.mu.Unlock()

	// 版本检查（必须在锁内，确保原子性）
	key := eventAggregateKey(aggregateType, aggregateID)
	currentVersion, err := m.getAggregateVersionUnsafe(key)
	if err != nil {
		return err
	}
	if currentVersion != expectedVersion {
		return errors.NewCode(errors.Concurrency,
			fmt.Sprintf("concurrency conflict: aggregate=%v, expected=%d, actual=%d", aggregateID, expectedVersion, currentVersion),
		).WithContext("aggregate_id", aggregateID).
			WithContext("expected_version", expectedVersion).
			WithContext("actual_version", currentVersion)
	}

	// 初始化存储（如果需要）
	if m.events[key] == nil {
		m.events[key] = make([]eventing.Event[int64], 0, len(convertedEvents))
	}
	if m.eventsByID[aggregateID] == nil {
		m.eventsByID[aggregateID] = make([]eventing.Event[int64], 0, len(convertedEvents))
	}

	// 快速写入（已转换好的事件）
	m.events[key] = append(m.events[key], convertedEvents...)
	m.eventsByID[aggregateID] = append(m.eventsByID[aggregateID], convertedEvents...)

	return nil
}

func (m *MemoryEventStore) LoadEvents(ctx context.Context, aggregateID int64, afterVersion uint64) ([]eventing.Event[int64], error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	aggregateEvents, ok := m.eventsByID[aggregateID]
	if !ok || len(aggregateEvents) == 0 {
		return []eventing.Event[int64]{}, nil
	}

	// 性能优化：事件在插入时已保证按版本有序，使用二分查找替代线性扫描
	// 时间复杂度从 O(n) 降低到 O(log n)，内存分配从 O(n) 降低到 O(1)
	startIdx := sort.Search(len(aggregateEvents), func(i int) bool {
		return aggregateEvents[i].GetVersion() > afterVersion
	})

	if startIdx >= len(aggregateEvents) {
		return []eventing.Event[int64]{}, nil
	}

	// 直接返回切片引用，避免额外的内存分配和拷贝
	// 注意：调用方不应修改返回的切片
	return aggregateEvents[startIdx:], nil
}

// LoadEventsByType 返回指定聚合类型下、某个版本之后的事件。
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

// StreamAggregate 按版本顺序分页读取单个聚合的事件流。
func (m *MemoryEventStore) StreamAggregate(ctx context.Context, opts *AggregateStreamOptions[int64]) (*AggregateStreamResult[int64], error) {
	if opts == nil {
		return nil, errors.NewCode(errors.InvalidInput, "AggregateStreamOptions cannot be nil")
	}
	if opts.AggregateID <= 0 {
		return nil, errors.NewCode(errors.InvalidInput, fmt.Sprintf("invalid aggregate id %d", opts.AggregateID))
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

// StreamEvents 对全局事件集应用过滤和游标分页。
func (m *MemoryEventStore) StreamEvents(ctx context.Context, opts *StreamOptions) (*StreamResult[int64], error) {
	m.mu.RLock()
	var all []eventing.Event[int64]
	for _, arr := range m.events {
		all = append(all, arr...)
	}
	m.mu.RUnlock()

	result := FilterEventsWithOptions[int64](all, opts)
	return result, nil
}

// HasAggregate 通过是否已存在事件流判断聚合是否存在。
func (m *MemoryEventStore) HasAggregate(ctx context.Context, aggregateID int64) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	events, ok := m.eventsByID[aggregateID]
	return ok && len(events) > 0, nil
}

func (m *MemoryEventStore) GetAggregateVersion(ctx context.Context, aggregateID int64) (uint64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	events, ok := m.eventsByID[aggregateID]
	if !ok || len(events) == 0 {
		return 0, nil
	}
	return events[len(events)-1].GetVersion(), nil
}

// eventAggregateKey 生成内部使用的 `aggregateType:aggregateID` 复合键。
func eventAggregateKey(aggregateType string, aggregateID int64) string {
	return fmt.Sprintf("%s:%d", aggregateType, aggregateID)
}

// getAggregateVersionUnsafe 在持锁前提下读取某个复合键对应的最新版本号。
func (m *MemoryEventStore) getAggregateVersionUnsafe(key string) (uint64, error) {
	aggregateEvents, exists := m.events[key]
	if !exists || len(aggregateEvents) == 0 {
		return 0, nil
	}
	return aggregateEvents[len(aggregateEvents)-1].GetVersion(), nil
}

// 编译期断言：确保 MemoryEventStore 实现 IEventStreamStore[int64]。
var _ IEventStreamStore[int64] = (*MemoryEventStore)(nil)
