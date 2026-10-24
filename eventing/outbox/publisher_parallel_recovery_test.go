package outbox

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"gochen/errors"
	"gochen/eventing"
	"gochen/observe/logging"
	"gochen/testkit/assert"
	"gochen/testkit/require"
)

// TestParallelPublisher_MarkFailed_MovesToDLQWhenMaxRetriesExceeded 验证 ParallelPublisher MarkFailed MovesToDLQWhenMaxRetriesExceeded。
func TestParallelPublisher_MarkFailed_MovesToDLQWhenMaxRetriesExceeded(t *testing.T) {
	repo := &dlqRecordingRepo{}
	bus := &concurrentEventBus{}

	cfg := OutboxConfig{
		RetryInterval: 30 * time.Second,
		MaxRetries:    3,
	}

	reg := newTestRegistry(t)
	upgraders := newTestUpgraders()
	p, err := NewParallelPublisher(repo, bus, cfg, logging.NewNoopLogger(), 1, reg, upgraders)
	require.NoError(t, err)
	dlq := &dlqRecordingDLQ{}
	require.NoError(t, p.SetDLQRepository(dlq))

	entry := OutboxEntry[int64]{
		ID:         42,
		RetryCount: cfg.MaxRetries - 1, // 再失败一次即达到上限
	}

	ctx := context.Background()
	require.NoError(t, p.markFailure(ctx, p.core().buildFailureMark(entry, "test-error")))

	// 应该调用 MarkAsFailed 一次
	require.Equal(t, 1, repo.FailedCount())

	// 且应移入 DLQ
	moved := dlq.Moved()
	require.Len(t, moved, 1)
	assert.Equal(t, int64(42), moved[0].ID)
	assert.Equal(t, cfg.MaxRetries, moved[0].RetryCount)
	assert.Equal(t, "test-error", moved[0].LastError)
}

func TestParallelPublisher_MarkPublishedRetriesWithFreshContextAfterSuccessfulPublish(t *testing.T) {
	entry := newProcessingOutboxEntry(t, 1)
	repo := &contextSensitiveMarkRepo{}
	ctx, cancel := context.WithCancel(context.Background())
	var published int32
	bus := &MockEventBus{
		publishEventFunc: func(context.Context, eventing.IEvent) error {
			atomic.AddInt32(&published, 1)
			cancel()
			return nil
		},
	}
	p := &ParallelPublisher[int64]{
		repo:          repo,
		bus:           bus,
		cfg:           DefaultOutboxConfig(),
		log:           logging.NewNoopLogger(),
		eventRegistry: newTestRegistry(t),
		upgraders:     newTestUpgraders(),
	}

	p.processEntry(ctx, entry)

	require.Equal(t, int32(1), atomic.LoadInt32(&published))
	require.GreaterOrEqual(t, atomic.LoadInt32(&repo.markPublishedCalls), int32(2))
	require.Equal(t, int32(1), atomic.LoadInt32(&repo.markPublishedSuccesses))
}

func TestParallelPublisher_ProcessEntryPublishedMarkFailureRecoversClaim(t *testing.T) {
	entry := newProcessingOutboxEntry(t, 21)
	repo := &MockOutboxRepository{
		markPublishError: assert.AnError,
	}
	bus := &MockEventBus{}
	p := &ParallelPublisher[int64]{
		repo:          repo,
		bus:           bus,
		cfg:           OutboxConfig{RetryInterval: time.Minute, MaxRetries: 3},
		log:           logging.NewNoopLogger(),
		eventRegistry: newTestRegistry(t),
		upgraders:     newTestUpgraders(),
	}

	p.processEntry(context.Background(), entry)

	require.Equal(t, 1, bus.PublishedEventsLen())
	require.Equal(t, 0, repo.MarkedPublishedLen())
	// 发布成功但标记发布失败时，mark 失败必须计入 RetryCount（有界重投 + DLQ 逃生），
	// 不能滞留 PROCESSING 被租约周期无界重复投递且永不进入终态。
	require.Equal(t, 1, repo.MarkedFailedLen())
}

func TestParallelPublisher_ProcessEntryRenewFailureStillMarksPublishedAfterSuccessfulPublish(t *testing.T) {
	repo := &MockOutboxRepository{
		renewClaimError: errors.NewCode(errors.Dependency, "lease lost"),
	}
	bus := &MockEventBus{
		publishEventFunc: func(ctx context.Context, event eventing.IEvent) error {
			// 模拟“发布已成功进入 bus，但续租刚好失败并取消了 run 上下文”的竞态：
			// PublishEvent 仍返回 nil（事件已投递）。
			<-ctx.Done()
			return nil
		},
	}
	entry := newProcessingOutboxEntry(t, 7)
	p := &ParallelPublisher[int64]{
		repo:          repo,
		bus:           bus,
		cfg:           OutboxConfig{BatchSize: 10, RetryInterval: time.Minute, MaxRetries: 3, ClaimLease: 50 * time.Millisecond, ClaimRenewInterval: 5 * time.Millisecond},
		log:           logging.NewNoopLogger(),
		eventRegistry: newTestRegistry(t),
		upgraders:     newTestUpgraders(),
	}

	p.processEntry(context.Background(), entry)

	// 发布成功即便续租失败也必须 markPublished，避免重复投递；且不得误标为 failed。
	require.Equal(t, 1, repo.MarkedPublishedLen())
	require.Equal(t, 0, repo.MarkedFailedLen())
}
