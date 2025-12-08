package main

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

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

// NewStringMemoryEventStore 创建一个新的字符串 ID 内存事件存储。
func NewStringMemoryEventStore() *StringMemoryEventStore {
	return &StringMemoryEventStore{
		events: make(map[string][]eventing.Event[string]),
	}
}

// AppendEvents 追加事件到指定聚合的事件流。
//
// 语义：
//   - 使用 expectedVersion 做乐观锁控制；
//   - 确保事件版本号是从 expectedVersion+1 开始的连续序列；
//   - 对于示例用途，发生并发冲突时直接返回错误。
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

// LoadEvents 加载指定聚合的事件历史。
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

// StreamEvents 按时间流式读取所有事件。
func (m *StringMemoryEventStore) StreamEvents(ctx context.Context, from time.Time) ([]eventing.Event[string], error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var res []eventing.Event[string]
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

// 编译期断言：确保实现了 IEventStore[string] 接口。
var _ estore.IEventStore[string] = (*StringMemoryEventStore)(nil)

