package outbox

import (
	"context"
	"sync"
	"testing"
	"time"

	"gochen/testkit/assert"

	"gochen/eventing"
	"gochen/observe/logging"
)

// TestPublisher_Start_Stop 验证 Publisher Start Stop。
func TestPublisher_Start_Stop(t *testing.T) {
	repo := &MockOutboxRepository{}
	eventBus := &MockEventBus{}
	cfg := OutboxConfig{
		PublishInterval: 100 * time.Millisecond,
		BatchSize:       10,
		RetryInterval:   30 * time.Second,
		RetentionPeriod: 24 * time.Hour,
	}

	reg := newTestRegistry(t)
	upgraders := newTestUpgraders()
	publisher, err := NewPublisher(repo, eventBus, cfg, logging.NewNoopLogger(), reg, upgraders)
	assert.NoError(t, err)

	ctx := context.Background()

	err = publisher.Start(ctx)
	assert.NoError(t, err)

	evt := newTestEvent(1, 1, "event-bg", nil)
	_ = repo.SaveWithEvents(ctx, 1, []eventing.Event[int64]{evt})

	time.Sleep(200 * time.Millisecond)
	assert.GreaterOrEqual(t, eventBus.PublishedEventsLen(), 1)

	err = publisher.Stop(ctx)
	assert.NoError(t, err)

	time.Sleep(100 * time.Millisecond)
	prevLen := eventBus.PublishedEventsLen()

	evt2 := newTestEvent(2, 1, "event-after-stop", nil)
	_ = repo.SaveWithEvents(ctx, 2, []eventing.Event[int64]{evt2})

	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, prevLen, eventBus.PublishedEventsLen())
}

// TestPublisher_ContextCancellation 验证 Publisher ContextCancellation。
func TestPublisher_ContextCancellation(t *testing.T) {
	repo := &MockOutboxRepository{}
	eventBus := &MockEventBus{}
	cfg := OutboxConfig{
		PublishInterval: 100 * time.Millisecond,
		BatchSize:       10,
		RetryInterval:   30 * time.Second,
		RetentionPeriod: 24 * time.Hour,
	}

	reg := newTestRegistry(t)
	upgraders := newTestUpgraders()
	publisher, err := NewPublisher(repo, eventBus, cfg, logging.NewNoopLogger(), reg, upgraders)
	assert.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())

	err = publisher.Start(ctx)
	assert.NoError(t, err)

	cancel()
	time.Sleep(150 * time.Millisecond)

	select {
	case <-publisher.doneCh:
	default:
		t.Error("Publisher should have stopped after context cancellation")
	}
}

// TestPublisher_CleanupPublished 验证 Publisher CleanupPublished。
func TestPublisher_CleanupPublished(t *testing.T) {
	repo := &MockOutboxRepository{}
	eventBus := &MockEventBus{}
	cfg := OutboxConfig{
		PublishInterval: 50 * time.Millisecond,
		CleanupInterval: 30 * time.Millisecond,
		BatchSize:       10,
		RetryInterval:   30 * time.Second,
		RetentionPeriod: 1 * time.Second,
	}

	reg := newTestRegistry(t)
	upgraders := newTestUpgraders()
	publisher, err := NewPublisher(repo, eventBus, cfg, logging.NewNoopLogger(), reg, upgraders)
	assert.NoError(t, err)

	ctx := context.Background()

	err = publisher.Start(ctx)
	assert.NoError(t, err)

	assert.Eventually(t, repo.DeletedPublished, 200*time.Millisecond, 10*time.Millisecond)

	_ = publisher.Stop(ctx)
}

func TestPublisher_CleanupInterval(t *testing.T) {
	repo := &MockOutboxRepository{}
	eventBus := &MockEventBus{}
	cfg := OutboxConfig{
		PublishInterval: 20 * time.Millisecond,
		CleanupInterval: 200 * time.Millisecond,
		BatchSize:       10,
		RetryInterval:   30 * time.Second,
		RetentionPeriod: 1 * time.Second,
	}

	reg := newTestRegistry(t)
	upgraders := newTestUpgraders()
	publisher, err := NewPublisher(repo, eventBus, cfg, logging.NewNoopLogger(), reg, upgraders)
	assert.NoError(t, err)

	ctx := context.Background()

	err = publisher.Start(ctx)
	assert.NoError(t, err)

	assert.Eventually(t, func() bool {
		return repo.DeletedPublishedCount() > 0
	}, 300*time.Millisecond, 10*time.Millisecond)

	_ = publisher.Stop(ctx)
}

func TestPublisher_ConcurrentStopWaitsForManualPublish(t *testing.T) {
	repo := &MockOutboxRepository{}
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	eventBus := &MockEventBus{publishEventFunc: func(context.Context, eventing.IEvent) error {
		once.Do(func() { close(entered) })
		<-release
		return nil
	}}
	cfg := OutboxConfig{PublishInterval: time.Hour, CleanupInterval: time.Hour, BatchSize: 1, RetryInterval: time.Second, RetentionPeriod: time.Hour}
	publisher, err := NewPublisher(repo, eventBus, cfg, logging.NewNoopLogger(), newTestRegistry(t), newTestUpgraders())
	assert.NoError(t, err)
	ctx := context.Background()
	assert.NoError(t, publisher.Start(ctx))
	assert.NoError(t, repo.SaveWithEvents(ctx, 1, []eventing.Event[int64]{newTestEvent(1, 1, "stop-barrier", nil)}))

	publishDone := make(chan error, 1)
	go func() { publishDone <- publisher.PublishPending(ctx) }()
	<-entered

	stopDone := make(chan error, 2)
	go func() { stopDone <- publisher.Stop(ctx) }()
	go func() { stopDone <- publisher.Stop(ctx) }()
	for i := 0; i < 2; i++ {
		select {
		case err := <-stopDone:
			t.Fatalf("Stop returned before PublishPending completed: %v", err)
		case <-time.After(20 * time.Millisecond):
		}
	}

	close(release)
	assert.NoError(t, <-publishDone)
	assert.NoError(t, <-stopDone)
	assert.NoError(t, <-stopDone)
}
