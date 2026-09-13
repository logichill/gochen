package store

import (
	"context"
	stdErrors "errors"
	"sync"

	gerrors "gochen/errors"
	"gochen/eventing"
)

// LoadAllEvents 加载聚合所有事件（版本从 0 开始）。
func LoadAllEvents[ID comparable](ctx context.Context, store IEventStore[ID], aggregateType string, aggregateID ID) ([]eventing.Event[ID], error) {
	return store.LoadEvents(ctx, aggregateType, aggregateID, 0)
}

// LoadAggregateEventsBatch 并发加载多个聚合的完整事件流。
func LoadAggregateEventsBatch[ID comparable](ctx context.Context, store IEventStore[ID], aggregateType string, aggregateIDs []ID, concurrency int) (map[ID][]eventing.Event[ID], error) {
	if concurrency <= 0 {
		concurrency = 5
	}

	results := make(map[ID][]eventing.Event[ID])
	var mutex sync.Mutex
	var wg sync.WaitGroup
	errChan := make(chan error, len(aggregateIDs))

	workerCount := concurrency
	if workerCount > len(aggregateIDs) {
		workerCount = len(aggregateIDs)
	}
	if workerCount == 0 {
		return results, nil
	}

	jobs := make(chan ID)
	for range workerCount {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for id := range jobs {
				// Prefer cancel over starting another load when the scheduler
				// and a free worker race after ctx is canceled.
				if err := ctx.Err(); err != nil {
					errChan <- gerrors.Wrap(err, gerrors.Dependency, "load aggregate canceled").
						WithContext("aggregate_id", id)
					continue
				}

				events, err := store.LoadEvents(ctx, aggregateType, id, 0)
				if err != nil {
					if appErr, ok := err.(*gerrors.AppError); ok && appErr != nil {
						errChan <- appErr.WithContext("aggregate_id", id)
					} else {
						errChan <- gerrors.Wrap(err, gerrors.Dependency, "load aggregate failed").
							WithContext("aggregate_id", id)
					}
					continue
				}

				mutex.Lock()
				results[id] = events
				mutex.Unlock()
			}
		}()
	}

schedule:
	for _, aggregateID := range aggregateIDs {
		// Prefer cancellation over scheduling when both are ready; a bare
		// select between Done and send is fair and can over-schedule.
		select {
		case <-ctx.Done():
			errChan <- gerrors.Wrap(ctx.Err(), gerrors.Dependency, "load aggregates canceled")
			break schedule
		default:
		}
		select {
		case <-ctx.Done():
			errChan <- gerrors.Wrap(ctx.Err(), gerrors.Dependency, "load aggregates canceled")
			break schedule
		case jobs <- aggregateID:
		}
	}
	close(jobs)

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
