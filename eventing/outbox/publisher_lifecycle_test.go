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
	publisher, err := newTestPublisher(repo, eventBus, cfg, logging.NewNoopLogger(), reg, upgraders)
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
	publisher, err := newTestPublisher(repo, eventBus, cfg, logging.NewNoopLogger(), reg, upgraders)
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
	publisher, err := newTestPublisher(repo, eventBus, cfg, logging.NewNoopLogger(), reg, upgraders)
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
	publisher, err := newTestPublisher(repo, eventBus, cfg, logging.NewNoopLogger(), reg, upgraders)
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
	publisher, err := newTestPublisher(repo, eventBus, cfg, logging.NewNoopLogger(), newTestRegistry(t), newTestUpgraders())
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

// TestPublisher_StopThenStart_RestartsAfterDefensiveStop 锁定生命周期语义：
// 未启动即 Stop 不进入不可逆终态——与 README（"启动前 Stop 不影响后续启动"）和
// ParallelPublisher 一致，防御性 Stop 后必须仍可 Start 并正常发布。
func TestPublisher_StopThenStart_RestartsAfterDefensiveStop(t *testing.T) {
	repo := &MockOutboxRepository{}
	eventBus := &MockEventBus{}
	cfg := OutboxConfig{
		PublishInterval: 50 * time.Millisecond,
		BatchSize:       10,
		RetryInterval:   30 * time.Second,
		RetentionPeriod: 24 * time.Hour,
	}
	publisher, err := newTestPublisher(repo, eventBus, cfg, logging.NewNoopLogger(), newTestRegistry(t), newTestUpgraders())
	assert.NoError(t, err)

	ctx := context.Background()

	// 未启动即 Stop：幂等返回 nil，不置终态。
	assert.NoError(t, publisher.Stop(ctx))
	assert.NoError(t, publisher.Stop(ctx))

	// 防御性 Stop 后仍可 Start，且后台循环正常发布。
	assert.NoError(t, publisher.Start(ctx))
	assert.NoError(t, repo.SaveWithEvents(ctx, 1, []eventing.Event[int64]{newTestEvent(1, 1, "event-after-defensive-stop", nil)}))
	assert.Eventually(t, func() bool {
		return eventBus.PublishedEventsLen() >= 1
	}, 2*time.Second, 10*time.Millisecond)

	// 手动 PublishPending 在启动前后均可用（终态未被置位）。
	assert.NoError(t, repo.SaveWithEvents(ctx, 2, []eventing.Event[int64]{newTestEvent(2, 1, "event-manual", nil)}))
	assert.NoError(t, publisher.PublishPending(ctx))
	assert.GreaterOrEqual(t, eventBus.PublishedEventsLen(), 2)

	assert.NoError(t, publisher.Stop(ctx))
}

// TestPublisher_StartThenStop_IsTerminal 锁定一次性语义的另一面：
// 正常启动并停止后进入终止态，再次 Start 必须要求创建新实例。
func TestPublisher_StartThenStop_IsTerminal(t *testing.T) {
	publisher, err := newTestPublisher(&MockOutboxRepository{}, &MockEventBus{}, DefaultOutboxConfig(), logging.NewNoopLogger(), newTestRegistry(t), newTestUpgraders())
	assert.NoError(t, err)

	ctx := context.Background()
	assert.NoError(t, publisher.Start(ctx))
	assert.NoError(t, publisher.Stop(ctx))
	assert.Error(t, publisher.Start(ctx))
}
