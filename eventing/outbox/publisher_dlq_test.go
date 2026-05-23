package outbox

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"gochen/errors"
	"gochen/logging"
)

// TestPublisher_DeserializeError_MoveToDLQWhenMaxRetriesExceeded 验证 Publisher DeserializeError MoveToDLQWhenMaxRetriesExceeded。
func TestPublisher_DeserializeError_MoveToDLQWhenMaxRetriesExceeded(t *testing.T) {
	maxRetries := 3

	past := time.Now().Add(-1 * time.Minute)
	repo := &MockOutboxRepository{
		entries: []OutboxEntry[int64]{
			{
				ID:          1,
				AggregateID: 123,
				EventID:     "event-invalid",
				EventType:   "TestEvent",
				EventData:   "invalid json {{{",
				Status:      OutboxStatusFailed,
				CreatedAt:   time.Now().Add(-2 * time.Minute),
				RetryCount:  maxRetries - 1,
				NextRetryAt: &past,
			},
		},
	}
	eventBus := &MockEventBus{}
	cfg := OutboxConfig{
		BatchSize:     10,
		RetryInterval: 30 * time.Second,
		MaxRetries:    maxRetries,
	}

	reg := newTestRegistry(t)
	upgraders := newTestUpgraders()
	publisher, err := NewPublisher(repo, eventBus, cfg, logging.NewNoopLogger(), reg, upgraders)
	assert.NoError(t, err)
	dlq := &MockDLQRepository{}
	publisher.SetDLQRepository(dlq)

	ctx := context.Background()
	err = publisher.PublishPending(ctx)
	assert.NoError(t, err)

	assert.Equal(t, 1, repo.MarkedFailedLen())
	moved := dlq.MovedEntries()
	if assert.Len(t, moved, 1) {
		assert.Equal(t, int64(1), moved[0].ID)
		assert.Equal(t, maxRetries, moved[0].RetryCount)
		assert.NotEmpty(t, moved[0].LastError)
	}
}

func TestPublisher_DeserializeError_DoesNotMoveToDLQWhenMarkFailedFails(t *testing.T) {
	maxRetries := 3

	past := time.Now().Add(-1 * time.Minute)
	repo := &MockOutboxRepository{
		entries: []OutboxEntry[int64]{
			{
				ID:          1,
				AggregateID: 123,
				EventID:     "event-invalid",
				EventType:   "TestEvent",
				EventData:   "invalid json {{{",
				Status:      OutboxStatusFailed,
				CreatedAt:   time.Now().Add(-2 * time.Minute),
				RetryCount:  maxRetries - 1,
				NextRetryAt: &past,
			},
		},
		markFailedError: errors.NewCode(errors.Conflict, "claim is no longer owned"),
	}
	eventBus := &MockEventBus{}
	cfg := OutboxConfig{
		BatchSize:     10,
		RetryInterval: 30 * time.Second,
		MaxRetries:    maxRetries,
	}

	reg := newTestRegistry(t)
	upgraders := newTestUpgraders()
	publisher, err := NewPublisher(repo, eventBus, cfg, logging.NewNoopLogger(), reg, upgraders)
	assert.NoError(t, err)
	dlq := &MockDLQRepository{}
	publisher.SetDLQRepository(dlq)

	err = publisher.PublishPending(context.Background())
	assert.Error(t, err)
	assert.Equal(t, 0, repo.MarkedFailedLen())
	assert.Empty(t, dlq.MovedEntries())
}

// TestPublisher_DeserializeError_NotMovedToDLQBelowMaxRetries 验证 Publisher DeserializeError NotMovedToDLQBelowMaxRetries。
func TestPublisher_DeserializeError_NotMovedToDLQBelowMaxRetries(t *testing.T) {
	maxRetries := 3

	past := time.Now().Add(-1 * time.Minute)
	repo := &MockOutboxRepository{
		entries: []OutboxEntry[int64]{
			{
				ID:          1,
				AggregateID: 123,
				EventID:     "event-invalid",
				EventType:   "TestEvent",
				EventData:   "invalid json {{{",
				Status:      OutboxStatusFailed,
				CreatedAt:   time.Now().Add(-2 * time.Minute),
				RetryCount:  0,
				NextRetryAt: &past,
			},
		},
	}
	eventBus := &MockEventBus{}
	cfg := OutboxConfig{
		BatchSize:     10,
		RetryInterval: 30 * time.Second,
		MaxRetries:    maxRetries,
	}

	reg := newTestRegistry(t)
	upgraders := newTestUpgraders()
	publisher, err := NewPublisher(repo, eventBus, cfg, logging.NewNoopLogger(), reg, upgraders)
	assert.NoError(t, err)
	dlq := &MockDLQRepository{}
	publisher.SetDLQRepository(dlq)

	ctx := context.Background()
	err = publisher.PublishPending(ctx)
	assert.NoError(t, err)

	assert.Equal(t, 1, repo.MarkedFailedLen())
	assert.Len(t, dlq.MovedEntries(), 0)
}
