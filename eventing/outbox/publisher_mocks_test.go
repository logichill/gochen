package outbox

import (
	"context"
	"sync"
	"time"

	"gochen/eventing"
	"gochen/eventing/bus"
	"gochen/messaging"
)

// MockOutboxRepository 模拟 Outbox 仓储
type MockOutboxRepository struct {
	mu                 sync.Mutex
	entries            []OutboxEntry[int64]
	markedPublished    []int64
	markedFailed       []int64
	renewedClaims      []int64
	renewClaimFunc     func(ctx context.Context, entryID int64, claimToken string) error
	deletedPublished   bool
	getPendingError    error
	markPublishError   error
	markFailedError    error
	renewClaimError    error
	deletePublishError error
}

// SaveWithEvents ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
// - aggregateID：对象/实体标识
// - events：事件列表（待追加/发布）（类型：[]eventing.Event[int64]）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (m *MockOutboxRepository) SaveWithEvents(ctx context.Context, aggregateID int64, events []eventing.Event[int64]) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, evt := range events {
		entry, _ := EventToOutboxEntry(aggregateID, evt)
		entry.ID = int64(len(m.entries) + 1)
		m.entries = append(m.entries, *entry)
	}
	return nil
}

// ClaimPendingEntries 从存储中 claim 一批实体。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - limit：分页大小（最大返回条数）
//
// 返回：
// - result1：列表结果（元素类型：OutboxEntry[int64]）
// - err：错误信息（nil 表示成功）
func (m *MockOutboxRepository) ClaimPendingEntries(ctx context.Context, limit int) ([]OutboxEntry[int64], error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getPendingError != nil {
		return nil, m.getPendingError
	}

	var pending []OutboxEntry[int64]
	for i := range m.entries {
		entry := &m.entries[i]
		if entry.Status == OutboxStatusPending ||
			(entry.Status == OutboxStatusFailed &&
				(entry.NextRetryAt == nil || !entry.NextRetryAt.After(time.Now()))) {
			entry.Status = OutboxStatusProcessing
			entry.ClaimToken = "mock-claim"
			leaseUntil := time.Now().Add(time.Minute)
			entry.LeaseUntil = &leaseUntil
			entry.NextRetryAt = nil
			pending = append(pending, *entry)
			if len(pending) >= limit {
				break
			}
		}
	}
	return pending, nil
}

// MarkAsPublished ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
// - entryID：对象/实体标识
//
// 返回：
// - err：错误信息（nil 表示成功）
func (m *MockOutboxRepository) MarkAsPublished(ctx context.Context, entryID int64, claimToken string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.markPublishError != nil {
		return m.markPublishError
	}
	m.markedPublished = append(m.markedPublished, entryID)
	for i := range m.entries {
		if m.entries[i].ID == entryID && m.entries[i].ClaimToken == claimToken {
			m.entries[i].Status = OutboxStatusPublished
			m.entries[i].ClaimToken = ""
			m.entries[i].LeaseUntil = nil
			now := time.Now()
			m.entries[i].PublishedAt = &now
			break
		}
	}
	return nil
}

// MarkAsFailed ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
// - entryID：对象/实体标识
// - errorMsg：错误信息（类型：string）
// - nextRetryAt：参数值（具体语义见函数上下文）（类型：time.Time）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (m *MockOutboxRepository) MarkAsFailed(ctx context.Context, entryID int64, claimToken string, errorMsg string, nextRetryAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.markFailedError != nil {
		return m.markFailedError
	}
	m.markedFailed = append(m.markedFailed, entryID)
	for i := range m.entries {
		if m.entries[i].ID == entryID && m.entries[i].ClaimToken == claimToken {
			m.entries[i].Status = OutboxStatusFailed
			m.entries[i].ClaimToken = ""
			m.entries[i].LeaseUntil = nil
			m.entries[i].LastError = errorMsg
			m.entries[i].NextRetryAt = &nextRetryAt
			m.entries[i].RetryCount++
			break
		}
	}
	return nil
}

// RenewClaim 延长已 claim 记录的 lease。
func (m *MockOutboxRepository) RenewClaim(ctx context.Context, entryID int64, claimToken string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.renewClaimFunc != nil {
		return m.renewClaimFunc(ctx, entryID, claimToken)
	}
	if m.renewClaimError != nil {
		return m.renewClaimError
	}
	m.renewedClaims = append(m.renewedClaims, entryID)
	for i := range m.entries {
		if m.entries[i].ID == entryID && m.entries[i].ClaimToken == claimToken {
			leaseUntil := time.Now().Add(defaultClaimLease)
			m.entries[i].LeaseUntil = &leaseUntil
			break
		}
	}
	return nil
}

// DeletePublished 删除对象并同步到存储。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - olderThan：阈值（用于过滤更早的数据）（类型：time.Time）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (m *MockOutboxRepository) DeletePublished(ctx context.Context, olderThan time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deletePublishError != nil {
		return m.deletePublishError
	}
	m.deletedPublished = true
	return nil
}

// MarkedPublishedLen 只读辅助方法（用于测试断言）。
//
// 说明：
//
// 返回：
// - result：数量/计数
func (m *MockOutboxRepository) MarkedPublishedLen() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.markedPublished)
}

// MarkedFailedLen result：数量/计数。
//
// 返回：
func (m *MockOutboxRepository) MarkedFailedLen() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.markedFailed)
}

// DeletedPublished 删除实体并同步到存储。
//
// 返回：
// - result：是否满足条件
func (m *MockOutboxRepository) DeletedPublished() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.deletedPublished
}

func (m *MockOutboxRepository) RenewedClaimsLen() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.renewedClaims)
}

// MockEventBus 模拟事件总线
type MockEventBus struct {
	mu               sync.Mutex
	publishedEvents  []eventing.Event[int64]
	publishError     error
	publishEventFunc func(ctx context.Context, event eventing.IEvent) error
}

// Publish 发布消息到消息总线。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - message：消息数据
//
// 返回：
// - err：错误信息（nil 表示成功）
func (m *MockEventBus) Publish(ctx context.Context, message messaging.IMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.publishError != nil {
		return m.publishError
	}
	if evt, ok := message.(*eventing.Event[int64]); ok {
		m.publishedEvents = append(m.publishedEvents, *evt)
	}
	return nil
}

// PublishAll 发布消息到消息总线。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - messages：消息数据
//
// 返回：
// - err：错误信息（nil 表示成功）
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

// PublishEvent 发布事件到事件总线。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - event：事件数据
//
// 返回：
// - err：错误信息（nil 表示成功）
func (m *MockEventBus) PublishEvent(ctx context.Context, event eventing.IEvent) error {
	if m.publishEventFunc != nil {
		return m.publishEventFunc(ctx, event)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.publishError != nil {
		return m.publishError
	}
	if evt, ok := event.(*eventing.Event[int64]); ok {
		m.publishedEvents = append(m.publishedEvents, *evt)
	}
	return nil
}

// PublishEvents 批量发布事件到事件总线。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - events：事件数据
//
// 返回：
// - err：错误信息（nil 表示成功）
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

// Subscribe 订阅消息并注册处理器。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - msgType：参数值（具体语义见函数上下文）（类型：string）
// - handler：事件处理器
//
// 返回：
// - result1：取消订阅函数（调用后解除订阅）
// - err：错误信息（nil 表示成功）
func (m *MockEventBus) Subscribe(ctx context.Context, msgType string, handler messaging.IMessageHandler) (messaging.UnsubscribeFunc, error) {
	return func(ctx context.Context) error { return nil }, nil
}

// Use 追加中间件。
//
// 参数：
// - middleware：中间件列表（类型：messaging.IMiddleware）
func (m *MockEventBus) Use(middleware messaging.IMiddleware) {}

// Handlers result：列表结果（元素类型：messaging.IMessageHandler）。
//
// 返回：
func (m *MockEventBus) Handlers() []messaging.IMessageHandler { return nil }

// SubscribeEvent 订阅指定类型的事件并注册处理器。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - eventType：事件类型
// - handler：事件处理器
//
// 返回：
// - result1：取消订阅函数（调用后解除订阅）
// - err：错误信息（nil 表示成功）
func (m *MockEventBus) SubscribeEvent(ctx context.Context, eventType string, handler bus.IEventHandler) (messaging.UnsubscribeFunc, error) {
	return func(ctx context.Context) error { return nil }, nil
}

// SubscribeHandler 按处理器声明的事件类型批量订阅。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - handler：事件处理器
//
// 返回：
// - result1：取消订阅函数（调用后解除订阅）
// - err：错误信息（nil 表示成功）
func (m *MockEventBus) SubscribeHandler(ctx context.Context, handler bus.IEventHandler) (messaging.UnsubscribeFunc, error) {
	return func(ctx context.Context) error { return nil }, nil
}

// publishLocked ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
// - message：消息数据
//
// 返回：
// - err：错误信息（nil 表示成功）
func (m *MockEventBus) publishLocked(ctx context.Context, message messaging.IMessage) error {
	if m.publishError != nil {
		return m.publishError
	}
	if evt, ok := message.(*eventing.Event[int64]); ok {
		m.publishedEvents = append(m.publishedEvents, *evt)
	}
	return nil
}

// publishEventLocked ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
// - event：事件数据
//
// 返回：
// - err：错误信息（nil 表示成功）
func (m *MockEventBus) publishEventLocked(ctx context.Context, event eventing.IEvent) error {
	if m.publishError != nil {
		return m.publishError
	}
	if evt, ok := event.(*eventing.Event[int64]); ok {
		m.publishedEvents = append(m.publishedEvents, *evt)
	}
	return nil
}

// PublishedEventsLen 发布消息到消息总线。
//
// 返回：
// - result：数量/计数
func (m *MockEventBus) PublishedEventsLen() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.publishedEvents)
}

var _ bus.IEventBus = (*MockEventBus)(nil)

// MockDLQRepository 简单的 DLQ 仓储，用于验证移入 DLQ 语义。
type MockDLQRepository struct {
	mu    sync.Mutex
	moved []OutboxEntry[int64]
}

// MoveToDLQ ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
// - entry：Outbox 条目
//
// 返回：
// - err：错误信息（nil 表示成功）
func (d *MockDLQRepository) MoveToDLQ(ctx context.Context, entry OutboxEntry[int64]) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.moved = append(d.moved, entry)
	return nil
}

// GetDLQEntries 从存储中查询实体。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - limit：分页大小（最大返回条数）
//
// 返回：
// - result1：列表结果（元素类型：DLQEntry[int64]）
// - err：错误信息（nil 表示成功）
func (d *MockDLQRepository) GetDLQEntries(ctx context.Context, limit int) ([]DLQEntry[int64], error) {
	return nil, nil
}

// RetryFromDLQ ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
// - entryID：对象/实体标识
//
// 返回：
// - err：错误信息（nil 表示成功）
func (d *MockDLQRepository) RetryFromDLQ(ctx context.Context, entryID int64) error { return nil }

// DeleteDLQEntry 删除实体并同步到存储。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - entryID：对象/实体标识
//
// 返回：
// - err：错误信息（nil 表示成功）
func (d *MockDLQRepository) DeleteDLQEntry(ctx context.Context, entryID int64) error { return nil }

// GetDLQCount 从存储中查询实体。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
//
// 返回：
// - result1：数值结果
// - err：错误信息（nil 表示成功）
func (d *MockDLQRepository) GetDLQCount(ctx context.Context) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return int64(len(d.moved)), nil
}

// MovedEntries result：列表结果（元素类型：OutboxEntry[int64]）。
//
// 返回：
func (d *MockDLQRepository) MovedEntries() []OutboxEntry[int64] {
	d.mu.Lock()
	defer d.mu.Unlock()
	cpy := make([]OutboxEntry[int64], len(d.moved))
	copy(cpy, d.moved)
	return cpy
}

var _ IDLQRepository[int64] = (*MockDLQRepository)(nil)
