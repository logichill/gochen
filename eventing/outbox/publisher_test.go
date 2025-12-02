package outbox

import (
	"context"
	"sync"
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
	mu                 sync.Mutex
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
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, evt := range events {
		entry, _ := EventToOutboxEntry(aggregateID, evt)
		entry.ID = int64(len(m.entries) + 1)
		m.entries = append(m.entries, *entry)
	}
	return nil
}

func (m *MockOutboxRepository) GetPendingEntries(ctx context.Context, limit int) ([]OutboxEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getPendingError != nil {
		return nil, m.getPendingError
	}

	var pending []OutboxEntry
	for _, entry := range m.entries {
		// 模拟 SQL 仓储语义：pending 或可重试的 failed 记录都会被视为“待处理”
		if entry.Status == OutboxStatusPending ||
			(entry.Status == OutboxStatusFailed &&
				(entry.NextRetryAt == nil || !entry.NextRetryAt.After(time.Now()))) {
			pending = append(pending, entry)
			if len(pending) >= limit {
				break
			}
		}
	}
	return pending, nil
}

func (m *MockOutboxRepository) MarkAsPublished(ctx context.Context, entryID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
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
	m.mu.Lock()
	defer m.mu.Unlock()
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
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deletePublishError != nil {
		return m.deletePublishError
	}
	m.deletedPublished = true
	return nil
}

// 只读辅助方法（用于测试断言）
func (m *MockOutboxRepository) MarkedPublishedLen() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.markedPublished)
}

func (m *MockOutboxRepository) MarkedFailedLen() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.markedFailed)
}

func (m *MockOutboxRepository) DeletedPublished() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.deletedPublished
}

// MockEventBus 模拟事件总线
type MockEventBus struct {
	mu              sync.Mutex
	publishedEvents []eventing.Event
	publishError    error
}

func (m *MockEventBus) Publish(ctx context.Context, message messaging.IMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.publishError != nil {
		return m.publishError
	}
	if evt, ok := message.(*eventing.Event); ok {
		m.publishedEvents = append(m.publishedEvents, *evt)
	}
	return nil
}

func (m *MockEventBus) PublishAll(ctx context.Context, messages []messaging.IMessage) error {
	m.mu.Lock()
	for _, msg := range messages {
		if err := m.publishLocked(ctx, msg); err != nil {
			m.mu.Unlock()
			return err
		}
	}
	m.mu.Unlock()
	return nil
}

func (m *MockEventBus) PublishEvent(ctx context.Context, event eventing.IEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.publishError != nil {
		return m.publishError
	}
	if evt, ok := event.(*eventing.Event); ok {
		m.publishedEvents = append(m.publishedEvents, *evt)
	}
	return nil
}

func (m *MockEventBus) PublishEvents(ctx context.Context, events []eventing.IEvent) error {
	m.mu.Lock()
	for _, evt := range events {
		if err := m.publishEventLocked(ctx, evt); err != nil {
			m.mu.Unlock()
			return err
		}
	}
	m.mu.Unlock()
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

// 内部锁保护的辅助方法
func (m *MockEventBus) publishLocked(ctx context.Context, message messaging.IMessage) error {
	if m.publishError != nil {
		return m.publishError
	}
	if evt, ok := message.(*eventing.Event); ok {
		m.publishedEvents = append(m.publishedEvents, *evt)
	}
	return nil
}

func (m *MockEventBus) publishEventLocked(ctx context.Context, event eventing.IEvent) error {
	if m.publishError != nil {
		return m.publishError
	}
	if evt, ok := event.(*eventing.Event); ok {
		m.publishedEvents = append(m.publishedEvents, *evt)
	}
	return nil
}

// 用于测试断言的只读方法
func (m *MockEventBus) PublishedEventsLen() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.publishedEvents)
}

// MockDLQRepository 简单的 DLQ 仓储，用于验证移入 DLQ 语义。
type MockDLQRepository struct {
	mu     sync.Mutex
	moved  []OutboxEntry
}

func (d *MockDLQRepository) MoveToDLQ(ctx context.Context, entry OutboxEntry) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.moved = append(d.moved, entry)
	return nil
}

func (d *MockDLQRepository) GetDLQEntries(ctx context.Context, limit int) ([]DLQEntry, error) {
	return nil, nil
}

func (d *MockDLQRepository) RetryFromDLQ(ctx context.Context, entryID int64) error {
	return nil
}

func (d *MockDLQRepository) DeleteDLQEntry(ctx context.Context, entryID int64) error {
	return nil
}

func (d *MockDLQRepository) GetDLQCount(ctx context.Context) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return int64(len(d.moved)), nil
}

// MovedEntries 返回已移入 DLQ 的 entry 副本（测试断言用）。
func (d *MockDLQRepository) MovedEntries() []OutboxEntry {
	d.mu.Lock()
	defer d.mu.Unlock()
	cpy := make([]OutboxEntry, len(d.moved))
	copy(cpy, d.moved)
	return cpy
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
	assert.Equal(t, 2, eventBus.PublishedEventsLen())
	assert.Equal(t, 2, repo.MarkedPublishedLen())
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
	assert.Equal(t, 0, eventBus.PublishedEventsLen())
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
	assert.Equal(t, 1, repo.MarkedFailedLen())
}

// 当反序列化失败且重试次数达到上限时，应该在标记失败的同时移入 DLQ。
func TestPublisher_DeserializeError_MoveToDLQWhenMaxRetriesExceeded(t *testing.T) {
	maxRetries := 3

	// 构造一条已失败多次、到达重试时间的记录
	past := time.Now().Add(-1 * time.Minute)
	repo := &MockOutboxRepository{
		entries: []OutboxEntry{
			{
				ID:          1,
				AggregateID: 123,
				EventID:     "event-invalid",
				EventType:   "TestEvent",
				EventData:   "invalid json {{{",
				Status:      OutboxStatusFailed,
				CreatedAt:   time.Now().Add(-2 * time.Minute),
				RetryCount:  maxRetries - 1, // 再失败一次即达到上限
				NextRetryAt: &past,          // 已到下次重试时间
			},
		},
	}
	eventBus := &MockEventBus{}
	cfg := OutboxConfig{
		BatchSize:     10,
		RetryInterval: 30 * time.Second,
		MaxRetries:    maxRetries,
	}

	publisher := NewPublisher(repo, eventBus, cfg, logging.NewNoopLogger())
	dlq := &MockDLQRepository{}
	publisher.SetDLQRepository(dlq)

	ctx := context.Background()
	err := publisher.PublishPending(ctx)
	assert.NoError(t, err)

	// 应该标记为失败
	assert.Equal(t, 1, repo.MarkedFailedLen())
	// 且应移入 DLQ
	moved := dlq.MovedEntries()
	if assert.Len(t, moved, 1) {
		assert.Equal(t, int64(1), moved[0].ID)
		assert.Equal(t, maxRetries, moved[0].RetryCount)
	}
}

// 当重试次数尚未达到上限时，只标记失败，不应移入 DLQ。
func TestPublisher_DeserializeError_NotMovedToDLQBelowMaxRetries(t *testing.T) {
	maxRetries := 3

	past := time.Now().Add(-1 * time.Minute)
	repo := &MockOutboxRepository{
		entries: []OutboxEntry{
			{
				ID:          1,
				AggregateID: 123,
				EventID:     "event-invalid",
				EventType:   "TestEvent",
				EventData:   "invalid json {{{",
				Status:      OutboxStatusFailed,
				CreatedAt:   time.Now().Add(-2 * time.Minute),
				RetryCount:  0,      // 远未达到上限
				NextRetryAt: &past,  // 已到重试时间
			},
		},
	}
	eventBus := &MockEventBus{}
	cfg := OutboxConfig{
		BatchSize:     10,
		RetryInterval: 30 * time.Second,
		MaxRetries:    maxRetries,
	}

	publisher := NewPublisher(repo, eventBus, cfg, logging.NewNoopLogger())
	dlq := &MockDLQRepository{}
	publisher.SetDLQRepository(dlq)

	ctx := context.Background()
	err := publisher.PublishPending(ctx)
	assert.NoError(t, err)

	// 标记失败一次
	assert.Equal(t, 1, repo.MarkedFailedLen())
	// 但不会移入 DLQ
	assert.Len(t, dlq.MovedEntries(), 0)
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
	assert.Equal(t, 1, repo.MarkedFailedLen())
	assert.Equal(t, 0, eventBus.PublishedEventsLen())
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
	assert.GreaterOrEqual(t, eventBus.PublishedEventsLen(), 1)

	// 停止 Publisher
	err = publisher.Stop()
	assert.NoError(t, err)

	// 验证后台任务已停止
	time.Sleep(100 * time.Millisecond)
	prevLen := eventBus.PublishedEventsLen()

	// 添加新事件，不应该被处理
	evt2 := newTestEvent(2, 1, "event-after-stop", nil)
	_ = repo.SaveWithEvents(ctx, 2, []eventing.Event{evt2})

	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, prevLen, eventBus.PublishedEventsLen()) // 长度不应增加
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
	assert.Equal(t, 1, eventBus.PublishedEventsLen())
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
	assert.True(t, repo.DeletedPublished())

	// 停止
	_ = publisher.Stop()
}
