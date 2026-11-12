package outbox

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"gochen/errors"
	"gochen/observe/logging"
	"gochen/testkit/require"
)

func TestParallelPublisher_StopDrainsQueuedWork(t *testing.T) {
	const entryCount = 20

	entries := make([]OutboxEntry[int64], 0, entryCount)
	for i := 0; i < entryCount; i++ {
		evt := newTestEvent(1, uint64(i+1), fmt.Sprintf("e-%d", i+1), map[string]any{"i": i})
		e, err := EventToOutboxEntry(evt.AggregateID, evt)
		require.NoError(t, err)
		e.ID = int64(i + 1)
		e.Status = OutboxStatusPending
		entries = append(entries, *e)
	}

	repo := newOneShotOutboxRepo(entries)
	slowBus := &slowPublishBus{delay: 10 * time.Millisecond}

	cfg := OutboxConfig{
		PublishInterval: 24 * time.Hour, // 测试中手动触发
		BatchSize:       entryCount,
		RetryInterval:   30 * time.Second,
		RetentionPeriod: time.Minute,
		MaxRetries:      3,
		CleanupInterval: 24 * time.Hour,
	}

	reg := newTestRegistry(t)
	upgraders := newTestUpgraders()
	p, err := newTestParallelPublisher(repo, slowBus, cfg, logging.NewNoopLogger(), 1, reg, upgraders)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, p.Start(ctx))
	require.NoError(t, p.PublishPending(ctx))
	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	require.NoError(t, p.Stop(stopCtx))

	require.Equal(t, int32(entryCount), atomic.LoadInt32(&slowBus.count))
}

func TestParallelPublisher_StopTimeoutCancelsRunningPublishContext(t *testing.T) {
	evt := newTestEvent(1, 1, "cancel-on-stop", map[string]any{"i": 1})
	entry, err := EventToOutboxEntry(evt.AggregateID, evt)
	require.NoError(t, err)
	entry.ID = 1
	entry.Status = OutboxStatusPending

	repo := newOneShotOutboxRepo([]OutboxEntry[int64]{*entry})
	blockingBus := &cancelAwarePublishBus{
		started:  make(chan struct{}),
		canceled: make(chan struct{}),
	}
	cfg := OutboxConfig{
		PublishInterval: 24 * time.Hour,
		BatchSize:       1,
		RetryInterval:   30 * time.Second,
		RetentionPeriod: time.Minute,
		MaxRetries:      3,
		CleanupInterval: 24 * time.Hour,
	}

	p, err := newTestParallelPublisher(repo, blockingBus, cfg, logging.NewNoopLogger(), 1, newTestRegistry(t), newTestUpgraders())
	require.NoError(t, err)
	require.NoError(t, p.SetBatchOperations(&recordingBatchOps{}))

	runCtx := context.Background()
	require.NoError(t, p.Start(runCtx))
	require.NoError(t, p.PublishPending(runCtx))

	require.Eventually(t, func() bool {
		select {
		case <-blockingBus.started:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer stopCancel()
	err = p.Stop(stopCtx)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Timeout))

	require.Eventually(t, func() bool {
		select {
		case <-blockingBus.canceled:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)

	select {
	case <-p.markDoneCh:
	case <-time.After(time.Second):
		t.Fatal("expected markLoop to stop after Stop timeout")
	}
}

func TestParallelPublisher_SettersRejectChangesAfterStart(t *testing.T) {
	repo := newOneShotOutboxRepo(nil)
	bus := &concurrentEventBus{}
	cfg := OutboxConfig{
		PublishInterval: 24 * time.Hour,
		BatchSize:       1,
		RetryInterval:   30 * time.Second,
		RetentionPeriod: time.Minute,
		MaxRetries:      3,
		CleanupInterval: 24 * time.Hour,
	}
	p, err := newTestParallelPublisher(repo, bus, cfg, logging.NewNoopLogger(), 1, newTestRegistry(t), newTestUpgraders())
	require.NoError(t, err)

	require.NoError(t, p.SetMetricsRecorder(nil))
	require.NoError(t, p.SetDLQRepository(nil))
	require.NoError(t, p.SetBatchOperations(nil))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, p.Start(ctx))

	require.True(t, errors.Is(p.SetMetricsRecorder(nil), errors.FailedPrecondition))
	require.True(t, errors.Is(p.SetDLQRepository(nil), errors.FailedPrecondition))
	require.True(t, errors.Is(p.SetBatchOperations(nil), errors.FailedPrecondition))

	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	require.NoError(t, p.Stop(stopCtx))
}
