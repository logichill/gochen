package outbox

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gochen/errors"
	"gochen/logging"
)

// TestPublisher_ConcurrentStartStopAndPublishPending_NoRace 验证 Publisher 在并发 Start/Stop/PublishPending 下无竞态与 panic。
func TestPublisher_ConcurrentStartStopAndPublishPending_NoRace(t *testing.T) {
	const entryCount = 200

	entries := make([]OutboxEntry[int64], 0, entryCount)
	for i := 0; i < entryCount; i++ {
		evt := newTestEvent(int64(i+1), 1, "TestEvent", nil)
		e, err := EventToOutboxEntry(evt.AggregateID, evt)
		require.NoError(t, err)
		e.ID = int64(i + 1)
		e.Status = OutboxStatusPending
		entries = append(entries, *e)
	}

	repo := newConcurrentOutboxRepo(entries)
	bus := &concurrentEventBus{}

	cfg := OutboxConfig{
		PublishInterval: 2 * time.Millisecond,
		BatchSize:       25,
		RetryInterval:   30 * time.Second,
		RetentionPeriod: time.Minute,
		MaxRetries:      3,
		CleanupInterval: 10 * time.Millisecond,
	}

	reg := newTestRegistry(t)
	upgraders := newTestUpgraders()
	p, err := NewPublisher(repo, bus, cfg, logging.NewNoopLogger(), reg, upgraders)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 并发 Start（幂等）
	{
		var wg sync.WaitGroup
		wg.Add(8)
		for i := 0; i < 8; i++ {
			go func() {
				defer wg.Done()
				_ = p.Start(ctx)
			}()
		}
		wg.Wait()
	}

	// 并发 PublishPending（与后台 loop 并行）；不追求强断言，只锁竞态。
	{
		var wg sync.WaitGroup
		wg.Add(8)
		for i := 0; i < 8; i++ {
			go func() {
				defer wg.Done()
				for j := 0; j < 30; j++ {
					_ = p.PublishPending(ctx)
				}
			}()
		}
		wg.Wait()
	}

	// 并发 Stop（幂等）
	{
		stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
		defer stopCancel()

		var wg sync.WaitGroup
		wg.Add(8)
		for i := 0; i < 8; i++ {
			go func() {
				defer wg.Done()
				_ = p.Stop(stopCtx)
			}()
		}
		wg.Wait()
	}

	// Stop 后不可再次 Start
	require.Error(t, p.Start(ctx))

	// Stop 后 PublishPending 应返回 INVALID_INPUT
	require.True(t, errors.Is(p.PublishPending(ctx), errors.InvalidInput))

	published, _ := repo.stats()
	require.Greater(t, published, 0, "expected some entries published")
}
