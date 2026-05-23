package store

import (
	"context"
	stdErrors "errors"
	"sync"

	gerrors "gochen/errors"
	"gochen/eventing"
)

// AggregateExists 检查聚合是否存在。
func AggregateExists[ID comparable](ctx context.Context, store IEventStore[ID], aggregateID ID) (bool, error) {
	return store.HasAggregate(ctx, aggregateID)
}

// GetCurrentVersion 从存储中查询事件。
//
// 说明：
// - GetCurrentVersion 获取聚合当前版本。
func GetCurrentVersion[ID comparable](ctx context.Context, store IEventStore[ID], aggregateID ID) (uint64, error) {
	return store.GetAggregateVersion(ctx, aggregateID)
}

// LoadAllEvents 解析事件。
//
// 说明：
// - LoadAllEvents 加载聚合所有事件。
func LoadAllEvents[ID comparable](ctx context.Context, store IEventStore[ID], aggregateID ID) ([]eventing.Event[ID], error) {
	return store.LoadEvents(ctx, aggregateID, 0)
}

// LoadAggregateEventsBatch 并发加载多个聚合的完整事件流。
func LoadAggregateEventsBatch[ID comparable](ctx context.Context, store IEventStore[ID], aggregateIDs []ID, concurrency int) (map[ID][]eventing.Event[ID], error) {
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
				if appErr, ok := err.(*gerrors.AppError); ok && appErr != nil {
					errChan <- appErr.WithContext("aggregate_id", id)
				} else {
					errChan <- gerrors.Wrap(err, gerrors.Dependency, "load aggregate failed").
						WithContext("aggregate_id", id)
				}
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
		return nil, gerrors.Wrap(stdErrors.Join(errs...), gerrors.Dependency, "load aggregates failed").
			WithContext("error_count", len(errs))
	}

	return results, nil
}
