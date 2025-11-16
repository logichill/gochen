package eventsourced

import (
	"context"
	"fmt"
	"time"

	"gochen/eventing"
	"gochen/eventing/bus"
	"gochen/logging"
	"gochen/messaging"
)

// RetryPolicy 重试策略
type RetryPolicy struct {
	MaxRetries int
	Backoff    time.Duration
}

// EventSourcedHandlerOption 配置事件处理器
type EventSourcedHandlerOption[T any] struct {
	Name        string
	EventType   string
	Decoder     func(event eventing.Event) (T, error)
	Handle      func(ctx context.Context, payload T) error
	RetryPolicy RetryPolicy
	Logger      logging.Logger
}

// EventSourcedTypedHandler 泛型事件处理模板
type EventSourcedTypedHandler[T any] struct {
	name      string
	eventType string
	decoder   func(event eventing.Event) (T, error)
	handle    func(ctx context.Context, payload T) error
	retry     RetryPolicy
	logger    logging.Logger
}

// NewEventSourcedTypedHandler 创建泛型事件处理器
func NewEventSourcedTypedHandler[T any](opt EventSourcedHandlerOption[T]) (*EventSourcedTypedHandler[T], error) {
	if opt.EventType == "" {
		return nil, fmt.Errorf("event type cannot be empty")
	}
	if opt.Handle == nil {
		return nil, fmt.Errorf("handler cannot be nil")
	}
	decoder := opt.Decoder
	if decoder == nil {
		decoder = func(evt eventing.Event) (T, error) {
			payload, ok := evt.Payload.(T)
			if !ok {
				var zero T
				return zero, fmt.Errorf("payload type mismatch: %T", evt.Payload)
			}
			return payload, nil
		}
	}

	handler := &EventSourcedTypedHandler[T]{
		name:      opt.Name,
		eventType: opt.EventType,
		decoder:   decoder,
		handle:    opt.Handle,
		retry:     opt.RetryPolicy,
		logger:    opt.Logger,
	}
	if handler.name == "" {
		handler.name = fmt.Sprintf("EventSourcedHandler<%s>", opt.EventType)
	}
	if handler.logger == nil {
		handler.logger = logging.GetLogger().WithFields(
			logging.String("component", "eventsourced.handler"),
			logging.String("handler", handler.name),
		)
	}
	return handler, nil
}

// Handle 兼容消息总线接口
func (h *EventSourcedTypedHandler[T]) Handle(ctx context.Context, message messaging.IMessage) error {
	evt, ok := message.(eventing.IEvent)
	if !ok {
		return fmt.Errorf("message is not an event: %T", message)
	}
	return h.HandleEvent(ctx, evt)
}

// HandleEvent 处理事件
func (h *EventSourcedTypedHandler[T]) HandleEvent(ctx context.Context, evt eventing.IEvent) error {
	domainEvent, ok := evt.(*eventing.Event)
	if !ok {
		return fmt.Errorf("event payload must be *eventing.Event, got %T", evt)
	}

	payload, err := h.decoder(*domainEvent)
	if err != nil {
		return fmt.Errorf("decode event payload failed: %w", err)
	}

	retries := h.retry.MaxRetries
	if retries < 0 {
		retries = 0
	}
	backoff := h.retry.Backoff
	if backoff <= 0 {
		backoff = 100 * time.Millisecond
	}

	var lastErr error
	for attempt := 0; attempt <= retries; attempt++ {
		if err := h.handle(ctx, payload); err != nil {
			lastErr = err
			if attempt == retries {
				break
			}
			time.Sleep(backoff)
			continue
		}
		return nil
	}

	h.logger.Warn(ctx, "event handler failed",
		logging.Error(lastErr),
		logging.String("event_type", h.eventType),
		logging.String("event_id", domainEvent.ID),
		logging.Int("retries", retries))
	return lastErr
}

// GetEventTypes 订阅的事件类型
func (h *EventSourcedTypedHandler[T]) GetEventTypes() []string {
	return []string{h.eventType}
}

// GetHandlerName 处理器名称
func (h *EventSourcedTypedHandler[T]) GetHandlerName() string {
	return h.name
}

// Type 兼容消息接口
func (h *EventSourcedTypedHandler[T]) Type() string {
	return h.eventType
}

// Ensure interface compliance
var _ bus.IEventHandler = (*EventSourcedTypedHandler[any])(nil)
