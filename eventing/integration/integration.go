package integration

import (
	"context"
	"fmt"
	"sync"
	"time"

	"gochen/eventing"
	"gochen/eventing/bus"
	"gochen/eventing/store"
	"gochen/logging"
	msg "gochen/messaging"
)

// IIntegrationEvent 集成事件接口
// 集成事件用于跨边界上下文（Bounded Context）的通信
// 与领域事件不同，集成事件是显式设计的公共契约
type IIntegrationEvent interface {
	eventing.IEvent
	IsIntegrationEvent() bool
	GetTargetContext() string // 目标上下文名称
	GetCorrelationID() string // 关联ID，用于追踪跨服务调用
	GetSourceContext() string // 来源上下文名称
}

// IntegrationEvent 集成事件基础结构
type IntegrationEvent struct {
	eventing.Event
	TargetContext string    `json:"target_context"` // 目标上下文
	CorrelationID string    `json:"correlation_id"` // 关联ID
	SourceContext string    `json:"source_context"` // 来源上下文
	PublishedAt   time.Time `json:"published_at"`   // 发布时间
}

// IsIntegrationEvent 标记为集成事件
func (e *IntegrationEvent) IsIntegrationEvent() bool {
	return true
}

// GetTargetContext 获取目标上下文
func (e *IntegrationEvent) GetTargetContext() string {
	return e.TargetContext
}

// GetCorrelationID 获取关联ID
func (e *IntegrationEvent) GetCorrelationID() string {
	return e.CorrelationID
}

// GetSourceContext 获取来源上下文
func (e *IntegrationEvent) GetSourceContext() string {
	return e.SourceContext
}

// NewIntegrationEvent 创建集成事件
func NewIntegrationEvent(
	aggregateID int64,
	aggregateType string,
	eventType string,
	version uint64,
	data map[string]any,
	targetContext string,
	sourceContext string,
	correlationID string,
) *IntegrationEvent {
	evt := eventing.NewEvent(aggregateID, aggregateType, eventType, version, data)

	return &IntegrationEvent{
		Event:         *evt,
		TargetContext: targetContext,
		SourceContext: sourceContext,
		CorrelationID: correlationID,
		PublishedAt:   time.Now(),
	}
}

// NewIntegrationEventWithoutCorrelation 创建没有关联ID的集成事件
func NewIntegrationEventWithoutCorrelation(
	aggregateID int64,
	aggregateType string,
	eventType string,
	version uint64,
	data map[string]any,
	targetContext string,
	sourceContext string,
) *IntegrationEvent {
	return NewIntegrationEvent(aggregateID, aggregateType, eventType, version, data, targetContext, sourceContext, "")
}

// IntegrationEventPublisher 集成事件发布器
// 负责将集成事件发布到外部系统（如消息队列、事件总线等）
type IntegrationEventPublisher struct {
	eventBus    bus.IEventBus     // 内部事件总线
	messageBus  msg.IMessageBus   // 外部消息总线（可选）
	eventStore  store.IEventStore // 集成事件存储（可选，用于确保至少一次投递）
	handlers    map[string][]IntegrationEventHandler
	mutex       sync.RWMutex
	enableStore bool // 是否启用集成事件持久化
	logger      logging.ILogger
}

// IntegrationEventHandler 集成事件处理器
type IntegrationEventHandler func(ctx context.Context, event IIntegrationEvent) error

// NewIntegrationEventPublisher 创建集成事件发布器
func NewIntegrationEventPublisher(
	eventBus bus.IEventBus,
	messageBus msg.IMessageBus,
	eventStore store.IEventStore,
) *IntegrationEventPublisher {
	publisher := &IntegrationEventPublisher{
		eventBus:    eventBus,
		messageBus:  messageBus,
		eventStore:  eventStore,
		handlers:    make(map[string][]IntegrationEventHandler),
		enableStore: false, // 默认不启用持久化
	}
	publisher.logger = logging.GetLogger().WithField("component", "integration.publisher")
	return publisher
}

// EnableEventStore 启用集成事件持久化
func (p *IntegrationEventPublisher) EnableEventStore() {
	p.enableStore = true
}

// DisableEventStore 禁用集成事件持久化
func (p *IntegrationEventPublisher) DisableEventStore() {
	p.enableStore = false
}

// Publish 发布集成事件
// 集成事件会被发布到内部事件总线和外部消息总线
func (p *IntegrationEventPublisher) Publish(ctx context.Context, event IIntegrationEvent) error {
	var logger logging.ILogger
	if p.logger != nil {
		logger = p.logger.WithField("event_type", event.GetType())
	}

	// 1. 可选：持久化集成事件（确保至少一次投递）
	if p.enableStore && p.eventStore != nil {
		evt := event.(*IntegrationEvent).Event
		// 使用版本0表示这是集成事件，不参与聚合版本控制
		if err := p.eventStore.AppendEvents(ctx, evt.AggregateID, []eventing.IStorableEvent{&evt}, 0); err != nil {
			return fmt.Errorf("failed to persist integration event: %w", err)
		}
	}

	// 2. 发布到内部事件总线（同一进程内的订阅者）
	if p.eventBus != nil {
		go func() {
			if err := p.eventBus.PublishEvent(ctx, event); err != nil {
				if logger != nil {
					logger.Error(ctx, "failed to publish integration event asynchronously", logging.Error(err))
				}
			}
		}()
	}

	// 3. 发布到外部消息总线（跨服务通信）
	if p.messageBus != nil {
		if err := p.messageBus.Publish(ctx, event); err != nil {
			return fmt.Errorf("failed to publish integration event to message bus: %w", err)
		}
	}

	// 4. 调用注册的处理器
	p.mutex.RLock()
	handlers := p.handlers[event.GetType()]
	p.mutex.RUnlock()

	for _, handler := range handlers {
		if err := handler(ctx, event); err != nil {
			if logger != nil {
				logger.Error(ctx, "integration event handler error", logging.Error(err))
			}
		}
	}

	return nil
}

// PublishAsync 异步发布集成事件
func (p *IntegrationEventPublisher) PublishAsync(ctx context.Context, event IIntegrationEvent) {
	logger := p.logger
	go func() {
		if err := p.Publish(ctx, event); err != nil {
			if logger != nil {
				logger.Error(ctx, "failed to publish integration event asynchronously", logging.Error(err))
			}
		}
	}()
}

// Subscribe 订阅集成事件
func (p *IntegrationEventPublisher) Subscribe(eventType string, handler IntegrationEventHandler) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	p.handlers[eventType] = append(p.handlers[eventType], handler)
}

// Unsubscribe 取消订阅（简化实现，实际项目中可能需要更复杂的订阅管理）
func (p *IntegrationEventPublisher) Unsubscribe(eventType string) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	delete(p.handlers, eventType)
}

// GetSubscribedEventTypes 获取已订阅的事件类型列表
func (p *IntegrationEventPublisher) GetSubscribedEventTypes() []string {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	types := make([]string, 0, len(p.handlers))
	for eventType := range p.handlers {
		types = append(types, eventType)
	}
	return types
}
