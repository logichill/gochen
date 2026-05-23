package outbox

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"gochen/eventing"
	"gochen/logging"
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

	time.Sleep(100 * time.Millisecond)
	assert.True(t, repo.DeletedPublished())

	_ = publisher.Stop(ctx)
}
