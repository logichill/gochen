package store

import (
	"context"
	"fmt"
	"sync"

	"gochen/eventing"
)

// AggregateExists 检查聚合是否存在。
//
// 说明：
//   - 优先使用 IAggregateInspector[ID]，否则退回到基于 Load 的检查；
//   - 仅依赖聚合级 IEventStore[ID]，不要求实现全局流接口。
func AggregateExists[ID comparable](ctx context.Context, store IEventStore[ID], aggregateID ID) (bool, error) {
	if inspector, ok := store.(IAggregateInspector[ID]); ok {
		return inspector.HasAggregate(ctx, aggregateID)
	}

	events, err := store.LoadEvents(ctx, aggregateID, 0)
	if err != nil {
		return false, err
	}
	return len(events) > 0, nil
}

// GetCurrentVersion 获取聚合当前版本。
//
// 说明：
//   - 优先使用 IAggregateInspector[ID]，否则退回到基于 Load 的检查；
//   - 当聚合不存在时返回 (0, nil)。
func GetCurrentVersion[ID comparable](ctx context.Context, store IEventStore[ID], aggregateID ID) (uint64, error) {
	if inspector, ok := store.(IAggregateInspector[ID]); ok {
		return inspector.GetAggregateVersion(ctx, aggregateID)
	}

	events, err := store.LoadEvents(ctx, aggregateID, 0)
	if err != nil {
		return 0, err
	}
	if len(events) == 0 {
		return 0, nil
	}
	return events[len(events)-1].GetVersion(), nil
}

// LoadAllEvents 加载聚合所有事件。
func LoadAllEvents[ID comparable](ctx context.Context, store IEventStore[ID], aggregateID ID) ([]eventing.Event[ID], error) {
	return store.LoadEvents(ctx, aggregateID, 0)
}

// LoadAggregatesHelper 并发加载多个聚合。
func LoadAggregatesHelper[ID comparable](ctx context.Context, store IEventStore[ID], aggregateIDs []ID, concurrency int) (map[ID][]eventing.Event[ID], error) {
	if concurrency <= 0 {
		concurrency = 5
	}

	results := make(map[ID][]eventing.Event[ID])
	var mutex sync.Mutex
	var wg sync.WaitGroup
	errChan := make(chan error, len(aggregateIDs))
	sem := make(chan struct{}, concurrency)

	for _, aggregateID := range aggregateIDs {
		wg.Add(1)
		go func(id ID) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			events, err := store.LoadEvents(ctx, id, 0)
			if err != nil {
				errChan <- fmt.Errorf("load aggregate %v failed: %w", id, err)
				return
			}

			mutex.Lock()
			results[id] = events
			mutex.Unlock()
		}(aggregateID)
	}

	wg.Wait()
	close(errChan)

	var errs []error
	for err := range errChan {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("load aggregates failed with %d errors, first error: %w", len(errs), errs[0])
	}

	return results, nil
}
