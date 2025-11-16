package store

import (
	"context"
	"fmt"
	"sync"

	"gochen/eventing"
)

// AggregateExists 检查聚合是否存在
func AggregateExists(ctx context.Context, store IEventStore, aggregateID int64) (bool, error) {
	if inspector, ok := store.(IAggregateInspector); ok {
		return inspector.HasAggregate(ctx, aggregateID)
	}
	if typed, ok := store.(ITypedEventStore); ok {
		return AggregateExistsWithType(ctx, typed, "", aggregateID)
	}

	events, err := store.LoadEvents(ctx, aggregateID, 0)
	if err != nil {
		return false, err
	}
	return len(events) > 0, nil
}

// AggregateExistsWithType 检查指定聚合类型是否存在事件
func AggregateExistsWithType(ctx context.Context, store ITypedEventStore, aggregateType string, aggregateID int64) (bool, error) {
	events, err := store.LoadEventsByType(ctx, aggregateType, aggregateID, 0)
	if err != nil {
		return false, err
	}
	return len(events) > 0, nil
}

// GetCurrentVersion 获取聚合当前版本
func GetCurrentVersion(ctx context.Context, store IEventStore, aggregateID int64) (uint64, error) {
	if inspector, ok := store.(IAggregateInspector); ok {
		return inspector.GetAggregateVersion(ctx, aggregateID)
	}
	if typed, ok := store.(ITypedEventStore); ok {
		return GetCurrentVersionWithType(ctx, typed, "", aggregateID)
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

// GetCurrentVersionWithType 获取指定聚合类型的版本
func GetCurrentVersionWithType(ctx context.Context, store ITypedEventStore, aggregateType string, aggregateID int64) (uint64, error) {
	events, err := store.LoadEventsByType(ctx, aggregateType, aggregateID, 0)
	if err != nil {
		return 0, err
	}
	if len(events) == 0 {
		return 0, nil
	}
	return events[len(events)-1].GetVersion(), nil
}

// LoadAllEvents 加载聚合所有事件
func LoadAllEvents(ctx context.Context, store IEventStore, aggregateID int64) ([]eventing.Event, error) {
	return store.LoadEvents(ctx, aggregateID, 0)
}

// LoadAggregatesHelper 并发加载多个聚合
func LoadAggregatesHelper(ctx context.Context, store IEventStore, aggregateIDs []int64, concurrency int) (map[int64][]eventing.Event, error) {
	if concurrency <= 0 {
		concurrency = 5
	}

	results := make(map[int64][]eventing.Event)
	var mutex sync.Mutex
	var wg sync.WaitGroup
	errChan := make(chan error, len(aggregateIDs))
	sem := make(chan struct{}, concurrency)

	for _, aggregateID := range aggregateIDs {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			events, err := store.LoadEvents(ctx, id, 0)
			if err != nil {
				errChan <- fmt.Errorf("load aggregate %d failed: %w", id, err)
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
		return nil, fmt.Errorf("load aggregates failed with %d errors: %v", len(errs), errs[0])
	}

	return results, nil
}
