package outbox

import (
	"context"
	"testing"
	"time"

	"gochen/testkit/assert"

	"gochen/errors"
	"gochen/observe/logging"
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
	assert.NoError(t, publisher.SetDLQRepository(dlq))

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
	assert.NoError(t, publisher.SetDLQRepository(dlq))

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
	assert.NoError(t, publisher.SetDLQRepository(dlq))

	ctx := context.Background()
	err = publisher.PublishPending(ctx)
	assert.NoError(t, err)

	assert.Equal(t, 1, repo.MarkedFailedLen())
	assert.Len(t, dlq.MovedEntries(), 0)
}

type failingAtomicDLQRepository struct {
	MockDLQRepository
	err error
}

func (d *failingAtomicDLQRepository) MarkFailedAndMoveToDLQ(ctx context.Context, entry OutboxEntry[int64], claimToken string, errorMsg string) error {
	return d.err
}

var _ IAtomicDLQMover[int64] = (*failingAtomicDLQRepository)(nil)

func TestPublisher_DeserializeError_FallsBackWhenAtomicDLQFails(t *testing.T) {
	maxRetries := 3
	past := time.Now().Add(-time.Minute)
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
	assert.NoError(t, publisher.SetDLQRepository(&failingAtomicDLQRepository{
		err: errors.NewCode(errors.Database, "atomic dlq failed"),
	}))

	err = publisher.PublishPending(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, 1, repo.MarkedFailedLen())
}

// TestPublisher_DeserializeError_NoMaxRetries_NeverMovesToDLQ 验证 NoMaxRetries(-1)
// 配置下反序列化失败只标记重试、永不转移到死信队列。
//
// 无重试上限意味着条目要一直重试到成功或人工介入；若 DLQ 判定误用
// RetryCount+1 >= MaxRetries（-1 恒成立），首次失败就会被误移入 DLQ，
// "无限重试"语义被静默破坏。
func TestPublisher_DeserializeError_NoMaxRetries_NeverMovesToDLQ(t *testing.T) {
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
		MaxRetries:    NoMaxRetries,
	}

	reg := newTestRegistry(t)
	upgraders := newTestUpgraders()
	publisher, err := NewPublisher(repo, eventBus, cfg, logging.NewNoopLogger(), reg, upgraders)
	assert.NoError(t, err)
	dlq := &MockDLQRepository{}
	assert.NoError(t, publisher.SetDLQRepository(dlq))

	ctx := context.Background()
	err = publisher.PublishPending(ctx)
	assert.NoError(t, err)

	// 只标记失败等待下轮重试，绝不进 DLQ。
	assert.Equal(t, 1, repo.MarkedFailedLen())
	assert.Empty(t, dlq.MovedEntries())
}

// TestPublisher_MarkPublishedError_MovesToDLQWhenRetriesExhausted 锁定 mark 失败的
// DLQ 逃生：发布成功但 MarkAsPublished 持续失败时，mark 失败计入 RetryCount，
// 达到上限后必须迁入死信队列，而不是以 PROCESSING 滞留被租约周期无限重投。
func TestPublisher_MarkPublishedError_MovesToDLQWhenRetriesExhausted(t *testing.T) {
	maxRetries := 3

	repo := &MockOutboxRepository{
		entries: []OutboxEntry[int64]{
			{
				ID:          1,
				AggregateID: 123,
				EventID:     "event-mark-fail",
				EventType:   "TestEvent",
				EventData:   `{"id":"event-mark-fail","type":"TestEvent","aggregate_id":123,"version":1}`,
				Status:      OutboxStatusPending,
				CreatedAt:   time.Now().Add(-2 * time.Minute),
				RetryCount:  maxRetries - 1,
			},
		},
		markPublishError: assert.AnError,
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
	assert.NoError(t, publisher.SetDLQRepository(dlq))

	ctx := context.Background()
	err = publisher.PublishPending(ctx)
	assert.Error(t, err)

	// 事件已投递，但 mark 失败被计入：Failed + RetryCount 达到上限 + 迁入 DLQ。
	assert.Equal(t, 1, eventBus.PublishedEventsLen())
	assert.Equal(t, 1, repo.MarkedFailedLen())
	moved := dlq.MovedEntries()
	if assert.Len(t, moved, 1) {
		assert.Equal(t, int64(1), moved[0].ID)
		assert.Equal(t, maxRetries, moved[0].RetryCount)
		assert.NotEmpty(t, moved[0].LastError)
	}
}
