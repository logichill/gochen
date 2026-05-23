package projection

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/bus"
	"gochen/eventing/store"
	"gochen/messaging"
)

type cursorGapEventStore struct {
	events []eventing.Event[int64]
}

// AppendEvents 向事件存储追加事件。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - aggregateID：对象/实体标识。
// - events：事件列表（待追加/发布）（类型：[]eventing.IStorableEvent[int64]）
// - expectedVersion：期望版本（用于乐观并发控制）（类型：uint64）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (s *cursorGapEventStore) AppendEvents(ctx context.Context, aggregateID int64, events []eventing.IStorableEvent[int64], expectedVersion uint64) error {
	return fmt.Errorf("not implemented")
}

// LoadEvents 解析数据。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - aggregateID：对象/实体标识。
// - afterVersion：起始版本（用于增量读取/追赶）（类型：uint64）
//
// 返回：
// - result1：列表结果（元素类型：eventing.Event[int64]）
// - err：错误信息（nil 表示成功）
func (s *cursorGapEventStore) LoadEvents(ctx context.Context, aggregateID int64, afterVersion uint64) ([]eventing.Event[int64], error) {
	return nil, fmt.Errorf("not implemented")
}

// LoadEventsByType 加载指定聚合类型的事件。
func (s *cursorGapEventStore) LoadEventsByType(
	ctx context.Context,
	aggregateType string,
	aggregateID int64,
	afterVersion uint64,
) ([]eventing.Event[int64], error) {
	return nil, fmt.Errorf("not implemented")
}

// HasAggregate 检查聚合是否存在。
func (s *cursorGapEventStore) HasAggregate(ctx context.Context, aggregateID int64) (bool, error) {
	return false, fmt.Errorf("not implemented")
}

// GetAggregateVersion 获取聚合当前版本。
func (s *cursorGapEventStore) GetAggregateVersion(ctx context.Context, aggregateID int64) (uint64, error) {
	return 0, fmt.Errorf("not implemented")
}

// StreamAggregate 遍历指定聚合的事件流。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - opts：可选参数/配置项。
//
// 返回：
// - result1：返回结果（类型：*store.AggregateStreamResult[int64]）
// - err：错误信息（nil 表示成功）
func (s *cursorGapEventStore) StreamAggregate(ctx context.Context, opts *store.AggregateStreamOptions[int64]) (*store.AggregateStreamResult[int64], error) {
	return nil, store.NewStreamNotSupportedError("StreamAggregate")
}

// StreamEvents 按游标遍历事件流。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - opts：可选参数/配置项。
//
// 返回：
// - result1：返回结果（类型：*store.StreamResult[int64]）
// - err：错误信息（nil 表示成功）
func (s *cursorGapEventStore) StreamEvents(ctx context.Context, opts *store.StreamOptions) (*store.StreamResult[int64], error) {
	if opts == nil {
		opts = &store.StreamOptions{}
	}

	var cursorTimestamp time.Time
	if opts.After != "" {
		found := false
		for i := range s.events {
			if s.events[i].ID == opts.After {
				cursorTimestamp = s.events[i].Timestamp
				found = true
				break
			}
		}
		if !found {
			return nil, errors.NewCode(errors.NotFound, "cursor not found").WithContext("cursor", opts.After)
		}
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 1000
	}
	queryLimit := limit + 1

	out := make([]eventing.Event[int64], 0, min(queryLimit, len(s.events)))
	for i := range s.events {
		e := s.events[i]

		if !opts.FromTime.IsZero() && e.Timestamp.Before(opts.FromTime) {
			continue
		}
		if len(opts.Types) > 0 && !containsString(opts.Types, e.Type) {
			continue
		}
		if opts.After != "" {
			if e.Timestamp.After(cursorTimestamp) || (e.Timestamp.Equal(cursorTimestamp) && strings.Compare(e.ID, opts.After) > 0) {
				// ok
			} else {
				continue
			}
		}

		out = append(out, e)
		if len(out) == queryLimit {
			break
		}
	}

	res := &store.StreamResult[int64]{Events: out}
	if len(out) == queryLimit {
		res.HasMore = true
		res.Events = out[:limit]
	}
	if len(res.Events) > 0 {
		res.NextCursor = res.Events[len(res.Events)-1].ID
	}
	return res, nil
}

func containsString(xs []string, v string) bool {
	for i := range xs {
		if xs[i] == v {
			return true
		}
	}
	return false
}

// min 执行对应操作。
//
// 参数：
// - b：参数值（具体语义见函数上下文）（类型：int）
//
// 返回：
// - result：数量/计数。
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// MockProjection 测试用投影。
type MockProjection struct {
	name                 string
	supportedTypes       []string
	handleFunc           func(ctx context.Context, event eventing.IEvent) error
	handleCheckpointFunc func(ctx context.Context, event eventing.IEvent, store ICheckpointStore, checkpoint *Checkpoint) error
	rebuildFunc          func(ctx context.Context, events []eventing.Event[int64]) error
	processedEvents      int
	failedEvents         int
	status               string
	mu                   sync.Mutex
}

var _ IProjection[int64] = (*MockProjection)(nil)

// NewMockProjection 创建并返回投影实例。
//
// 参数：
// - name：名称。
// - types：参数值（具体语义见函数上下文）（类型：[]string）
//
// 返回：
// - result：返回的实例（类型：*MockProjection）
func NewMockProjection(name string, types []string) *MockProjection {
	return &MockProjection{
		name:           name,
		supportedTypes: types,
		status:         "running",
	}
}

// GetName 返回当前值。
//
// 返回：
// - result：文本结果。
func (p *MockProjection) Name() string {
	return p.name
}

// Handle 处理消息并执行业务处理逻辑。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - event：事件数据。
//
// 返回：
// - err：错误信息（nil 表示成功）
func (p *MockProjection) Handle(ctx context.Context, event eventing.IEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.handleFunc != nil {
		err := p.handleFunc(ctx, event)
		if err != nil {
			p.failedEvents++
			return err
		}
	}
	p.processedEvents++
	return nil
}

// HandleWithCheckpoint 在测试中显式承载 checkpoint 模式。
func (p *MockProjection) HandleWithCheckpoint(ctx context.Context, event eventing.IEvent, store ICheckpointStore, checkpoint *Checkpoint) error {
	if p.handleCheckpointFunc != nil {
		return p.handleCheckpointFunc(ctx, event, store, checkpoint)
	}
	if err := p.Handle(ctx, event); err != nil {
		return err
	}
	if store != nil && checkpoint != nil {
		return store.Save(ctx, checkpoint)
	}
	return nil
}

// GetSupportedEventTypes 返回当前值。
//
// 返回：
// - result：列表结果（元素类型：string）
func (p *MockProjection) SupportedEventTypes() []string {
	return p.supportedTypes
}

// Rebuild ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
// - events：事件列表（待追加/发布）（类型：[]eventing.Event[int64]）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (p *MockProjection) Rebuild(ctx context.Context, events []eventing.Event[int64]) error {
	if p.rebuildFunc != nil {
		return p.rebuildFunc(ctx, events)
	}
	return nil
}

// RebuildWithCheckpoint 在测试中显式承载 rebuild checkpoint 模式。
func (p *MockProjection) RebuildWithCheckpoint(ctx context.Context, events []eventing.Event[int64], store ICheckpointStore, checkpoint *Checkpoint) error {
	if err := p.Rebuild(ctx, events); err != nil {
		return err
	}
	if store != nil && checkpoint != nil {
		return store.Save(ctx, checkpoint)
	}
	return nil
}

// GetStatus 返回当前值。
//
// 返回：
// - result：返回值（类型：ProjectionStatus）
func (p *MockProjection) Status() ProjectionStatus {
	p.mu.Lock()
	defer p.mu.Unlock()

	return ProjectionStatus{
		Name:            p.name,
		ProcessedEvents: int64(p.processedEvents),
		FailedEvents:    int64(p.failedEvents),
		Status:          p.status,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
}

// MockEventBus for testing
type MockEventBus struct {
	publishedEvents []eventing.IEvent
	handlers        map[string][]bus.IEventHandler
	mu              sync.Mutex
}

// PublishEvent 发布事件到事件总线。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - event：事件数据。
//
// 返回：
// - err：错误信息（nil 表示成功）
func (m *MockEventBus) PublishEvent(ctx context.Context, event eventing.IEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.publishedEvents == nil {
		m.publishedEvents = []eventing.IEvent{}
	}
	m.publishedEvents = append(m.publishedEvents, event)
	return nil
}

// PublishEvents 批量发布事件到事件总线。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - events：事件数据。
//
// 返回：
// - err：错误信息（nil 表示成功）
func (m *MockEventBus) PublishEvents(ctx context.Context, events []eventing.IEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.publishedEvents == nil {
		m.publishedEvents = []eventing.IEvent{}
	}

	// Simply append the events (they are already interfaces)
	m.publishedEvents = append(m.publishedEvents, events...)
	return nil
}

// PublishAll 发布消息到消息总线。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - messages：消息数据。
//
// 返回：
// - err：错误信息（nil 表示成功）
func (m *MockEventBus) PublishAll(ctx context.Context, messages []messaging.IMessage) error {
	// Convert messages to events and publish
	for _, msg := range messages {
		if evt, ok := msg.(eventing.IEvent); ok {
			m.PublishEvent(ctx, evt)
		}
	}
	return nil
}

// SubscribeEvent 订阅指定类型的事件并注册处理器。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - eventType：事件类型。
// - handler：事件处理器。
//
// 返回：
// - result1：取消订阅函数（调用后解除订阅）
// - err：错误信息（nil 表示成功）
func (m *MockEventBus) SubscribeEvent(ctx context.Context, eventType string, handler bus.IEventHandler) (messaging.UnsubscribeFunc, error) {
	m.mu.Lock()
	if m.handlers == nil {
		m.handlers = make(map[string][]bus.IEventHandler)
	}
	m.handlers[eventType] = append(m.handlers[eventType], handler)
	m.mu.Unlock()

	var once sync.Once
	return func(ctx context.Context) error {
		once.Do(func() {
			m.mu.Lock()
			defer m.mu.Unlock()
			handlers := m.handlers[eventType]
			for i := range handlers {
				if handlers[i] == handler {
					m.handlers[eventType] = append(handlers[:i], handlers[i+1:]...)
					break
				}
			}
		})
		return nil
	}, nil
}

// SubscribeHandler 按处理器声明的事件类型批量订阅。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - handler：事件处理器。
//
// 返回：
// - result1：取消订阅函数（调用后解除订阅）
// - err：错误信息（nil 表示成功）
func (m *MockEventBus) SubscribeHandler(ctx context.Context, handler bus.IEventHandler) (messaging.UnsubscribeFunc, error) {
	return func(ctx context.Context) error { return nil }, nil
}

// Publish 发布消息到消息总线。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - message：消息数据。
//
// 返回：
// - err：错误信息（nil 表示成功）
func (m *MockEventBus) Publish(ctx context.Context, message messaging.IMessage) error {
	return nil
}

// Subscribe 订阅消息并注册处理器。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - topic：参数值（具体语义见函数上下文）（类型：string）
// - handler：事件处理器。
//
// 返回：
// - result1：取消订阅函数（调用后解除订阅）
// - err：错误信息（nil 表示成功）
func (m *MockEventBus) Subscribe(ctx context.Context, topic string, handler messaging.IMessageHandler) (messaging.UnsubscribeFunc, error) {
	return func(ctx context.Context) error { return nil }, nil
}

// Use 追加中间件。
//
// 参数：
// - middleware：中间件列表（类型：messaging.IMiddleware）
func (m *MockEventBus) Use(middleware messaging.IMiddleware) {
	// No-op for mock
}

// toStorableEvents 将事件切片转换为存储接口切片（测试辅助）。
//
// 说明：
//
// 参数：
// - events：事件列表（待追加/发布）（类型：[]eventing.Event[int64]）
//
// 返回：
// - result：列表结果（元素类型：eventing.IStorableEvent[int64]）
func toStorableEvents(events []eventing.Event[int64]) []eventing.IStorableEvent[int64] {
	storable := make([]eventing.IStorableEvent[int64], len(events))
	for i := range events {
		storable[i] = &events[i]
	}
	return storable
}
