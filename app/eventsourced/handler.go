package eventsourced

import (
	"context"
	"fmt"
	"time"

	gerrors "gochen/errors"
	"gochen/eventing"
	"gochen/eventing/bus"
	"gochen/logging"
	"gochen/messaging"
	"gochen/policy/retry"
)

// RetryPolicy 定义事件处理失败时的重试参数。
type RetryPolicy struct {
	MaxRetries int
	Backoff    time.Duration
}

// EventSourcedHandlerOption 描述构建事件处理模板所需的配置。
type EventSourcedHandlerOption[T any] struct {
	Name        string
	EventType   string
	Decoder     func(event eventing.IEvent) (T, error)
	Handle      func(ctx context.Context, payload T) error
	RetryPolicy RetryPolicy
	Logger      logging.ILogger
}

// EventSourcedTypedHandler 提供 payload 解码、日志和重试的一体化事件处理模板。
type EventSourcedTypedHandler[T any] struct {
	name      string
	eventType string
	decoder   func(event eventing.IEvent) (T, error)
	handle    func(ctx context.Context, payload T) error
	retry     RetryPolicy
	logger    logging.ILogger
}

// NewEventSourcedTypedHandler 创建一个带解码器和重试策略的泛型事件处理器。
func NewEventSourcedTypedHandler[T any](opt EventSourcedHandlerOption[T]) (*EventSourcedTypedHandler[T], error) {
	if opt.EventType == "" {
		return nil, gerrors.NewCode(gerrors.InvalidInput, "event type cannot be empty")
	}
	if opt.Handle == nil {
		return nil, gerrors.NewCode(gerrors.InvalidInput, "handler cannot be nil")
	}
	decoder := opt.Decoder
	if decoder == nil {
		decoder = defaultPayloadDecoder[T]()
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
		handler.logger = logging.ComponentLogger("app.eventsourced.handler").
			WithField("handler", handler.name)
	}
	return handler, nil
}

// Handle 把总线消息校验为事件后转交给 HandleEvent。
func (h *EventSourcedTypedHandler[T]) Handle(ctx context.Context, message messaging.IMessage) error {
	evt, ok := message.(eventing.IEvent)
	if !ok {
		return gerrors.NewCode(gerrors.InvalidInput, "message is not an event").
			WithContext("message_type", fmt.Sprintf("%T", message))
	}
	return h.HandleEvent(ctx, evt)
}

// HandleEvent 解码事件 payload，并按配置的重试策略执行真正的处理逻辑。
func (h *EventSourcedTypedHandler[T]) HandleEvent(ctx context.Context, evt eventing.IEvent) error {
	if evt == nil {
		return gerrors.NewCode(gerrors.InvalidInput, "event cannot be nil")
	}

	payload, err := h.decoder(evt)
	if err != nil {
		return gerrors.Wrap(err, gerrors.InvalidInput, "decode event payload failed").
			WithContext("event_type", h.eventType).
			WithContext("event_id", evt.GetID())
	}

	retries := h.retry.MaxRetries
	if retries < 0 {
		retries = 0
	}
	backoff := h.retry.Backoff
	if backoff <= 0 {
		backoff = 100 * time.Millisecond
	}

	cfg := retry.Config{
		MaxAttempts:   retries + 1, // 含首次
		InitialDelay:  backoff,
		BackoffFactor: 2.0,
		MaxDelay:      30 * time.Second,
	}

	err = retry.Do(ctx, func(opCtx context.Context) error {
		return h.handle(opCtx, payload)
	}, cfg)
	if err == nil {
		return nil
	}
	if gerrors.Is(err, context.Canceled) || gerrors.Is(err, context.DeadlineExceeded) {
		return gerrors.Wrap(err, gerrors.Timeout, "retry canceled").
			WithContext("event_type", h.eventType).
			WithContext("event_id", evt.GetID())
	}

	h.logger.Warn(ctx, "event handler failed",
		logging.Error(err),
		logging.String("event_type", h.eventType),
		logging.String("event_id", evt.GetID()),
		logging.Int("retries", retries))
	return err
}

func (h *EventSourcedTypedHandler[T]) EventTypes() []string {
	return []string{h.eventType}
}

func (h *EventSourcedTypedHandler[T]) HandlerName() string {
	return h.name
}

func (h *EventSourcedTypedHandler[T]) Type() string {
	return h.eventType
}

// 编译期断言：确保 EventSourcedTypedHandler 实现 bus.IEventHandler。
var _ bus.IEventHandler = (*EventSourcedTypedHandler[any])(nil)
