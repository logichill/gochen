package outbox

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"gochen/testkit/assert"

	"gochen/eventing"
	"gochen/observe/logging"
)

// TestNewPublisher 验证 NewPublisher。
func TestNewPublisher(t *testing.T) {
	repo := &MockOutboxRepository{}
	eventBus := &MockEventBus{}
	cfg := DefaultOutboxConfig()
	reg := newTestRegistry(t)
	upgraders := newTestUpgraders()

	publisher, err := NewPublisher(repo, eventBus, cfg, nil, reg, upgraders)
	assert.NoError(t, err)
	assert.NotNil(t, publisher)
	assert.NotNil(t, publisher.log)
}

// TestPublisher_PublishPending 验证 Publisher PublishPending。
func TestPublisher_PublishPending(t *testing.T) {
	repo := &MockOutboxRepository{}
	eventBus := &MockEventBus{}
	cfg := OutboxConfig{
		BatchSize:     10,
		RetryInterval: 30 * time.Second,
	}

	evt1 := newTestEvent(1, 1, "event-1", nil)
	evt2 := newTestEvent(2, 1, "event-2", nil)
	ctx := context.Background()
	_ = repo.SaveWithEvents(ctx, 1, []eventing.Event[int64]{evt1, evt2})

	reg := newTestRegistry(t)
	upgraders := newTestUpgraders()
	publisher, err := NewPublisher(repo, eventBus, cfg, logging.NewNoopLogger(), reg, upgraders)
	assert.NoError(t, err)

	err = publisher.PublishPending(ctx)
	assert.NoError(t, err)

	assert.Equal(t, 2, eventBus.PublishedEventsLen())
	assert.Equal(t, 2, repo.MarkedPublishedLen())
	assert.Equal(t, 1, repo.DeletedPublishedCount())
}

// TestPublisher_PublishPending_EmptyQueue 验证 Publisher PublishPending EmptyQueue。
func TestPublisher_PublishPending_EmptyQueue(t *testing.T) {
	repo := &MockOutboxRepository{}
	eventBus := &MockEventBus{}
	cfg := DefaultOutboxConfig()

	reg := newTestRegistry(t)
	upgraders := newTestUpgraders()
	publisher, err := NewPublisher(repo, eventBus, cfg, logging.NewNoopLogger(), reg, upgraders)
	assert.NoError(t, err)

	ctx := context.Background()
	err = publisher.PublishPending(ctx)
	assert.NoError(t, err)
	assert.Equal(t, 0, eventBus.PublishedEventsLen())
}

// TestPublisher_PublishPending_InvalidEventData 验证 Publisher PublishPending InvalidEventData。
func TestPublisher_PublishPending_InvalidEventData(t *testing.T) {
	repo := &MockOutboxRepository{
		entries: []OutboxEntry[int64]{
			{
				ID:          1,
				AggregateID: 123,
				EventID:     "event-invalid",
				EventType:   "TestEvent",
				EventData:   "invalid json {{{",
				Status:      OutboxStatusPending,
				CreatedAt:   time.Now(),
				RetryCount:  0,
			},
		},
	}
	eventBus := &MockEventBus{}
	cfg := OutboxConfig{
		BatchSize:     10,
		RetryInterval: 30 * time.Second,
	}

	reg := newTestRegistry(t)
	upgraders := newTestUpgraders()
	publisher, err := NewPublisher(repo, eventBus, cfg, logging.NewNoopLogger(), reg, upgraders)
	assert.NoError(t, err)

	ctx := context.Background()
	err = publisher.PublishPending(ctx)
	assert.NoError(t, err)

	assert.Equal(t, 1, repo.MarkedFailedLen())
}

// TestPublisher_PublishPending_PublishError 验证 Publisher PublishPending PublishError。
func TestPublisher_PublishPending_PublishError(t *testing.T) {
	repo := &MockOutboxRepository{}
	eventBus := &MockEventBus{
		publishError: assert.AnError,
	}
	cfg := OutboxConfig{
		BatchSize:     10,
		RetryInterval: 30 * time.Second,
	}

	evt := newTestEvent(1, 1, "event-1", nil)
	ctx := context.Background()
	_ = repo.SaveWithEvents(ctx, 1, []eventing.Event[int64]{evt})

	reg := newTestRegistry(t)
	upgraders := newTestUpgraders()
	publisher, err := NewPublisher(repo, eventBus, cfg, logging.NewNoopLogger(), reg, upgraders)
	assert.NoError(t, err)

	err = publisher.PublishPending(ctx)
	assert.NoError(t, err)

	assert.Equal(t, 1, repo.MarkedFailedLen())
	assert.Equal(t, 0, eventBus.PublishedEventsLen())
}

// TestPublisher_MarkPublishedError 验证 Publisher MarkPublishedError。
func TestPublisher_MarkPublishedError(t *testing.T) {
	repo := &MockOutboxRepository{
		markPublishError: assert.AnError,
	}
	eventBus := &MockEventBus{}
	cfg := OutboxConfig{
		BatchSize:     10,
		RetryInterval: 30 * time.Second,
	}

	evt := newTestEvent(1, 1, "event-1", nil)
	ctx := context.Background()
	_ = repo.SaveWithEvents(ctx, 1, []eventing.Event[int64]{evt})

	reg := newTestRegistry(t)
	upgraders := newTestUpgraders()
	publisher, err := NewPublisher(repo, eventBus, cfg, logging.NewNoopLogger(), reg, upgraders)
	assert.NoError(t, err)

	err = publisher.PublishPending(ctx)
	assert.Error(t, err)

	assert.Equal(t, 1, eventBus.PublishedEventsLen())
	assert.Equal(t, 1, repo.MarkedFailedLen())
	assert.Equal(t, OutboxStatusFailed, repo.EntryStatus(1))
}

func TestPublisher_PublishPending_DeletesPublishedAfterSuccessfulBatch(t *testing.T) {
	repo := &MockOutboxRepository{}
	eventBus := &MockEventBus{}
	cfg := OutboxConfig{
		BatchSize:       10,
		RetryInterval:   30 * time.Second,
		CleanupInterval: 24 * time.Hour,
		RetentionPeriod: time.Nanosecond,
	}

	evt := newTestEvent(1, 1, "event-cleanup", nil)
	ctx := context.Background()
	_ = repo.SaveWithEvents(ctx, 1, []eventing.Event[int64]{evt})

	publisher, err := NewPublisher(repo, eventBus, cfg, logging.NewNoopLogger(), newTestRegistry(t), newTestUpgraders())
	assert.NoError(t, err)

	err = publisher.PublishPending(ctx)

	assert.NoError(t, err)
	assert.Equal(t, 1, repo.MarkedPublishedLen())
	assert.Equal(t, 1, repo.DeletedPublishedCount())
}

func TestPublisher_PublishPending_RenewsClaimDuringSlowPublish(t *testing.T) {
	repo := &MockOutboxRepository{}
	publishStarted := make(chan struct{})
	releasePublish := make(chan struct{})
	eventBus := &MockEventBus{
		publishEventFunc: func(ctx context.Context, event eventing.IEvent) error {
			close(publishStarted)
			select {
			case <-releasePublish:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}
	cfg := OutboxConfig{
		BatchSize:          10,
		RetryInterval:      30 * time.Second,
		ClaimLease:         50 * time.Millisecond,
		ClaimRenewInterval: 5 * time.Millisecond,
	}

	evt := newTestEvent(1, 1, "event-1", nil)
	ctx := context.Background()
	_ = repo.SaveWithEvents(ctx, 1, []eventing.Event[int64]{evt})

	reg := newTestRegistry(t)
	upgraders := newTestUpgraders()
	publisher, err := NewPublisher(repo, eventBus, cfg, logging.NewNoopLogger(), reg, upgraders)
	assert.NoError(t, err)

	done := make(chan error, 1)
	go func() {
		done <- publisher.PublishPending(ctx)
	}()

	<-publishStarted
	time.Sleep(20 * time.Millisecond)
	close(releasePublish)

	assert.NoError(t, <-done)
	assert.GreaterOrEqual(t, repo.RenewedClaimsLen(), 1)
	assert.Equal(t, 1, repo.MarkedPublishedLen())
}

func TestPublisher_PublishPending_IgnoresRenewCancellationAfterSuccessfulPublish(t *testing.T) {
	repo := &MockOutboxRepository{}
	renewStarted := make(chan struct{})
	repo.renewClaimFunc = func(ctx context.Context, entryID int64, claimToken string) error {
		select {
		case <-renewStarted:
		default:
			close(renewStarted)
		}
		<-ctx.Done()
		return ctx.Err()
	}

	eventBus := &MockEventBus{
		publishEventFunc: func(ctx context.Context, event eventing.IEvent) error {
			<-renewStarted
			return nil
		},
	}
	cfg := OutboxConfig{
		BatchSize:          10,
		RetryInterval:      30 * time.Second,
		ClaimLease:         50 * time.Millisecond,
		ClaimRenewInterval: 5 * time.Millisecond,
	}

	evt := newTestEvent(1, 1, "event-1", nil)
	ctx := context.Background()
	_ = repo.SaveWithEvents(ctx, 1, []eventing.Event[int64]{evt})

	reg := newTestRegistry(t)
	upgraders := newTestUpgraders()
	publisher, err := NewPublisher(repo, eventBus, cfg, logging.NewNoopLogger(), reg, upgraders)
	assert.NoError(t, err)

	err = publisher.PublishPending(ctx)
	assert.NoError(t, err)
	assert.Equal(t, 1, repo.MarkedPublishedLen())
}

func TestPublisher_PublishPending_RenewFailureStillMarksPublishedAfterSuccessfulPublish(t *testing.T) {
	renewErr := errors.New("lease lost")
	repo := &MockOutboxRepository{
		renewClaimError: renewErr,
	}
	eventBus := &MockEventBus{
		publishEventFunc: func(ctx context.Context, event eventing.IEvent) error {
			// 模拟“发布已成功进入 bus，但续租刚好失败并取消了 run 上下文”的竞态：
			// PublishEvent 仍返回 nil（事件已投递）。
			<-ctx.Done()
			return nil
		},
	}
	cfg := OutboxConfig{
		BatchSize:          10,
		RetryInterval:      30 * time.Second,
		ClaimLease:         50 * time.Millisecond,
		ClaimRenewInterval: 5 * time.Millisecond,
	}

	evt := newTestEvent(1, 1, "event-1", nil)
	ctx := context.Background()
	_ = repo.SaveWithEvents(ctx, 1, []eventing.Event[int64]{evt})

	publisher, err := NewPublisher(repo, eventBus, cfg, logging.NewNoopLogger(), newTestRegistry(t), newTestUpgraders())
	assert.NoError(t, err)

	err = publisher.PublishPending(ctx)
	// 续租失败仍作为独立信号上报，供上层观测租约健康度。
	assert.Error(t, err)
	assert.ErrorIs(t, err, renewErr)
	// 但事件已发布成功，必须落库 markPublished，避免被重新 claim 导致重复投递。
	assert.Equal(t, 1, repo.MarkedPublishedLen())
}

func TestPublisher_PublishPending_RetriesMarkPublishedAfterContextCancellation(t *testing.T) {
	repo := &contextSensitiveMarkRepo{claimEntries: []OutboxEntry[int64]{newProcessingOutboxEntry(t, 1)}}
	eventBus := &MockEventBus{}
	cfg := OutboxConfig{
		BatchSize:     10,
		RetryInterval: 30 * time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())

	eventBus.publishEventFunc = func(ctx context.Context, event eventing.IEvent) error {
		cancel()
		return nil
	}

	reg := newTestRegistry(t)
	upgraders := newTestUpgraders()
	publisher, err := NewPublisher(repo, eventBus, cfg, logging.NewNoopLogger(), reg, upgraders)
	assert.NoError(t, err)

	err = publisher.PublishPending(ctx)
	assert.NoError(t, err)
	assert.Equal(t, int32(2), atomic.LoadInt32(&repo.markPublishedCalls))
	assert.Equal(t, int32(1), atomic.LoadInt32(&repo.markPublishedSuccesses))
}
