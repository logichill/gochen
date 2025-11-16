package outbox

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"gochen/eventing"
	"gochen/eventing/bus"
	"gochen/logging"
	"gochen/messaging"
)

// MockOutboxRepository 模拟 Outbox 仓储
type MockOutboxRepository struct {
	entries            []OutboxEntry
	markedPublished    []int64
	markedFailed       []int64
	deletedPublished   bool
	getPendingError    error
	markPublishError   error
	markFailedError    error
	deletePublishError error
}

func (m *MockOutboxRepository) SaveWithEvents(ctx context.Context, aggregateID int64, events []eventing.Event) error {
	for _, evt := range events {
		entry, _ := EventToOutboxEntry(aggregateID, evt)
		entry.ID = int64(len(m.entries) + 1)
		m.entries = append(m.entries, *entry)
	}
	return nil
}

func (m *MockOutboxRepository) GetPendingEntries(ctx context.Context, limit int) ([]OutboxEntry, error) {
	if m.getPendingError != nil {
		return nil, m.getPendingError
	}

	var pending []OutboxEntry
	for _, entry := range m.entries {
		if entry.Status == OutboxStatusPending {
			pending = append(pending, entry)
			if len(pending) >= limit {
				break
			}
		}
	}
	return pending, nil
}

func (m *MockOutboxRepository) MarkAsPublished(ctx context.Context, entryID int64) error {
	if m.markPublishError != nil {
		return m.markPublishError
	}
	m.markedPublished = append(m.markedPublished, entryID)
	for i := range m.entries {
		if m.entries[i].ID == entryID {
			m.entries[i].Status = OutboxStatusPublished
			now := time.Now()
			m.entries[i].PublishedAt = &now
			break
		}
	}
	return nil
}

func (m *MockOutboxRepository) MarkAsFailed(ctx context.Context, entryID int64, errorMsg string, nextRetryAt time.Time) error {
	if m.markFailedError != nil {
		return m.markFailedError
	}
	m.markedFailed = append(m.markedFailed, entryID)
	for i := range m.entries {
		if m.entries[i].ID == entryID {
			m.entries[i].Status = OutboxStatusFailed
			m.entries[i].LastError = errorMsg
			m.entries[i].NextRetryAt = &nextRetryAt
			m.entries[i].RetryCount++
			break
		}
	}
	return nil
}

func (m *MockOutboxRepository) DeletePublished(ctx context.Context, olderThan time.Time) error {
	if m.deletePublishError != nil {
		return m.deletePublishError
	}
	m.deletedPublished = true
	return nil
}

// MockEventBus 模拟事件总线
type MockEventBus struct {
	publishedEvents []eventing.Event
	publishError    error
}

func (m *MockEventBus) Publish(ctx context.Context, message messaging.IMessage) error {
	if m.publishError != nil {
		return m.publishError
	}
	if evt, ok := message.(*eventing.Event); ok {
		m.publishedEvents = append(m.publishedEvents, *evt)
	}
	return nil
}

func (m *MockEventBus) PublishAll(ctx context.Context, messages []messaging.IMessage) error {
	for _, msg := range messages {
		if err := m.Publish(ctx, msg); err != nil {
			return err
		}
	}
	return nil
}

func (m *MockEventBus) PublishEvent(ctx context.Context, event eventing.IEvent) error {
	if m.publishError != nil {
		return m.publishError
	}
	if evt, ok := event.(*eventing.Event); ok {
		m.publishedEvents = append(m.publishedEvents, *evt)
	}
	return nil
}

func (m *MockEventBus) PublishEvents(ctx context.Context, events []eventing.IEvent) error {
	for _, evt := range events {
		if err := m.PublishEvent(ctx, evt); err != nil {
			return err
		}
	}
	return nil
}

func (m *MockEventBus) Subscribe(ctx context.Context, msgType string, handler messaging.IMessageHandler) error {
	return nil
}

func (m *MockEventBus) Unsubscribe(ctx context.Context, msgType string, handler messaging.IMessageHandler) error {
	return nil
}

func (m *MockEventBus) Use(middleware messaging.IMiddleware) {
}

func (m *MockEventBus) Handlers() []messaging.IMessageHandler {
	return nil
}

func (m *MockEventBus) SubscribeEvent(ctx context.Context, eventType string, handler bus.IEventHandler) error {
	return nil
}

func (m *MockEventBus) UnsubscribeEvent(ctx context.Context, eventType string, handler bus.IEventHandler) error {
	return nil
}

func (m *MockEventBus) SubscribeHandler(ctx context.Context, handler bus.IEventHandler) error {
	return nil
}

func (m *MockEventBus) UnsubscribeHandler(ctx context.Context, handler bus.IEventHandler) error {
	return nil
}

// ========== Publisher 测试 ==========

func TestNewPublisher(t *testing.T) {
	repo := &MockOutboxRepository{}
	eventBus := &MockEventBus{}
	cfg := DefaultOutboxConfig()

	publisher := NewPublisher(repo, eventBus, cfg, nil)
	assert.NotNil(t, publisher)
	assert.NotNil(t, publisher.log) // 应该创建默认 logger
}

func TestPublisher_PublishPending(t *testing.T) {
	repo := &MockOutboxRepository{}
	eventBus := &MockEventBus{}
	cfg := OutboxConfig{
		BatchSize:     10,
		RetryInterval: 30 * time.Second,
	}

	// 添加测试事件到仓储
	evt1 := newTestEvent(1, 1, "event-1", nil)
	evt2 := newTestEvent(2, 1, "event-2", nil)
	ctx := context.Background()
	_ = repo.SaveWithEvents(ctx, 1, []eventing.Event{evt1, evt2})

	publisher := NewPublisher(repo, eventBus, cfg, logging.NewNoopLogger())

	// 手动触发发布
	err := publisher.PublishPending(ctx)
	assert.NoError(t, err)

	// 验证事件已发布
	assert.Len(t, eventBus.publishedEvents, 2)
	assert.Len(t, repo.markedPublished, 2)
}

func TestPublisher_PublishPending_EmptyQueue(t *testing.T) {
	repo := &MockOutboxRepository{}
	eventBus := &MockEventBus{}
	cfg := DefaultOutboxConfig()

	publisher := NewPublisher(repo, eventBus, cfg, logging.NewNoopLogger())

	// 空队列不应报错
	ctx := context.Background()
	err := publisher.PublishPending(ctx)
	assert.NoError(t, err)
	assert.Len(t, eventBus.publishedEvents, 0)
}

func TestPublisher_PublishPending_InvalidEventData(t *testing.T) {
	repo := &MockOutboxRepository{
		entries: []OutboxEntry{
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

	publisher := NewPublisher(repo, eventBus, cfg, logging.NewNoopLogger())

	ctx := context.Background()
	err := publisher.PublishPending(ctx)
	assert.NoError(t, err) // 不应该返回错误，只标记失败

	// 验证标记为失败
	assert.Len(t, repo.markedFailed, 1)
	assert.Equal(t, int64(1), repo.markedFailed[0])
}

func TestPublisher_PublishPending_PublishError(t *testing.T) {
	repo := &MockOutboxRepository{}
	eventBus := &MockEventBus{
		publishError: assert.AnError,
	}
	cfg := OutboxConfig{
		BatchSize:     10,
		RetryInterval: 30 * time.Second,
	}

	// 添加测试事件
	evt := newTestEvent(1, 1, "event-1", nil)
	ctx := context.Background()
	_ = repo.SaveWithEvents(ctx, 1, []eventing.Event{evt})

	publisher := NewPublisher(repo, eventBus, cfg, logging.NewNoopLogger())

	// 发布失败应标记为失败
	err := publisher.PublishPending(ctx)
	assert.NoError(t, err) // 不应该返回错误

	// 验证标记为失败
	assert.Len(t, repo.markedFailed, 1)
	assert.Len(t, eventBus.publishedEvents, 0)
}

func TestPublisher_Start_Stop(t *testing.T) {
	repo := &MockOutboxRepository{}
	eventBus := &MockEventBus{}
	cfg := OutboxConfig{
		PublishInterval: 100 * time.Millisecond,
		BatchSize:       10,
		RetryInterval:   30 * time.Second,
		RetentionPeriod: 24 * time.Hour,
	}

	publisher := NewPublisher(repo, eventBus, cfg, logging.NewNoopLogger())

	ctx := context.Background()

	// 启动 Publisher
	err := publisher.Start(ctx)
	assert.NoError(t, err)

	// 添加事件
	evt := newTestEvent(1, 1, "event-bg", nil)
	_ = repo.SaveWithEvents(ctx, 1, []eventing.Event{evt})

	// 等待后台任务处理
	time.Sleep(200 * time.Millisecond)

	// 验证事件已发布
	assert.GreaterOrEqual(t, len(eventBus.publishedEvents), 1)

	// 停止 Publisher
	err = publisher.Stop()
	assert.NoError(t, err)

	// 验证后台任务已停止
	time.Sleep(100 * time.Millisecond)
	prevLen := len(eventBus.publishedEvents)

	// 添加新事件，不应该被处理
	evt2 := newTestEvent(2, 1, "event-after-stop", nil)
	_ = repo.SaveWithEvents(ctx, 2, []eventing.Event{evt2})

	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, prevLen, len(eventBus.publishedEvents)) // 长度不应增加
}

func TestPublisher_ContextCancellation(t *testing.T) {
	repo := &MockOutboxRepository{}
	eventBus := &MockEventBus{}
	cfg := OutboxConfig{
		PublishInterval: 100 * time.Millisecond,
		BatchSize:       10,
		RetryInterval:   30 * time.Second,
		RetentionPeriod: 24 * time.Hour,
	}

	publisher := NewPublisher(repo, eventBus, cfg, logging.NewNoopLogger())

	ctx, cancel := context.WithCancel(context.Background())

	// 启动 Publisher
	err := publisher.Start(ctx)
	assert.NoError(t, err)

	// 取消 context
	cancel()

	// 等待 goroutine 退出
	time.Sleep(150 * time.Millisecond)

	// doneCh 应该已关闭
	select {
	case <-publisher.doneCh:
		// 正常，已退出
	default:
		t.Error("Publisher should have stopped after context cancellation")
	}
}

func TestPublisher_MarkPublishedError(t *testing.T) {
	repo := &MockOutboxRepository{
		markPublishError: assert.AnError,
	}
	eventBus := &MockEventBus{}
	cfg := OutboxConfig{
		BatchSize:     10,
		RetryInterval: 30 * time.Second,
	}

	// 添加测试事件
	evt := newTestEvent(1, 1, "event-1", nil)
	ctx := context.Background()
	_ = repo.SaveWithEvents(ctx, 1, []eventing.Event{evt})

	// 清除 markPublishError 以便事件能发布
	repo.markPublishError = nil

	// 重新设置 markPublishError
	repo.markPublishError = assert.AnError

	publisher := NewPublisher(repo, eventBus, cfg, logging.NewNoopLogger())

	// 发布应该成功，但标记失败不应影响整体
	err := publisher.PublishPending(ctx)
	assert.NoError(t, err)

	// 事件应该已发布
	assert.Len(t, eventBus.publishedEvents, 1)
}

func TestPublisher_CleanupPublished(t *testing.T) {
	repo := &MockOutboxRepository{}
	eventBus := &MockEventBus{}
	cfg := OutboxConfig{
		PublishInterval: 50 * time.Millisecond,
		BatchSize:       10,
		RetryInterval:   30 * time.Second,
		RetentionPeriod: 1 * time.Second,
	}

	publisher := NewPublisher(repo, eventBus, cfg, logging.NewNoopLogger())

	ctx := context.Background()

	// 启动 Publisher
	err := publisher.Start(ctx)
	assert.NoError(t, err)

	// 等待至少一次清理
	time.Sleep(100 * time.Millisecond)

	// 验证清理已执行
	assert.True(t, repo.deletedPublished)

	// 停止
	_ = publisher.Stop()
}
