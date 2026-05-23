package bus

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"gochen/errors"
	"gochen/eventing"
	"gochen/messaging"
	"gochen/observe"
)

// TypedEventHandler 类型化事件处理器。
//
// 提供泛型支持的事件处理器（Go 1.18+），
// 自动完成 IMessage → IEvent → Payload 的三层类型转换，
// 允许处理特定 Payload 类型的事件。
//
// T 为事件 Payload 类型。
type TypedEventHandler[T any] struct {
	handleFunc  func(ctx context.Context, evt eventing.IEvent, payload T) error
	eventTypes  []string
	handlerName string
	metrics     observe.IMetrics
}

// NewTypedEventHandler 创建一个会自动解码 payload 的泛型事件处理器。
func NewTypedEventHandler[T any](
	handlerName string,
	eventTypes []string,
	fn func(ctx context.Context, evt eventing.IEvent, payload T) error,
) *TypedEventHandler[T] {
	if len(eventTypes) == 0 {
		eventTypes = []string{"*"}
	}
	if fn == nil {
		fn = func(context.Context, eventing.IEvent, T) error {
			return errors.NewCode(errors.InvalidInput, "typed event handler func is nil")
		}
	}
	return &TypedEventHandler[T]{
		handleFunc:  fn,
		eventTypes:  eventTypes,
		handlerName: handlerName,
	}
}

// WithMetrics 为处理器接入耗时和成功率等指标采集。
func (h *TypedEventHandler[T]) WithMetrics(metrics observe.IMetrics) *TypedEventHandler[T] {
	h.metrics = metrics
	return h
}

// Handle 把消息校验为事件后转交给 HandleEvent。
func (h *TypedEventHandler[T]) Handle(ctx context.Context, message messaging.IMessage) error {
	evt, ok := message.(eventing.IEvent)
	if !ok {
		return errors.NewCode(errors.InvalidInput, "message is not an event").
			WithContext("message_type", fmt.Sprintf("%T", message))
	}
	return h.HandleEvent(ctx, evt)
}

// HandleEvent 把事件 payload 解码为目标类型并执行真正的业务处理逻辑。
func (h *TypedEventHandler[T]) HandleEvent(ctx context.Context, evt eventing.IEvent) error {
	if h == nil || h.handleFunc == nil {
		return errors.NewCode(errors.InvalidInput, "typed event handler is nil")
	}
	if evt == nil {
		return errors.NewCode(errors.InvalidInput, "evt is nil")
	}
	payload, ok := messaging.PayloadAs[T](evt.GetPayload())
	if !ok {
		expectedType := reflect.TypeFor[T]()
		return errors.NewCode(errors.InvalidInput, "invalid payload type").
			WithContext("expected_payload_type", expectedType.String()).
			WithContext("actual_payload_type", evt.GetPayload().TypeName())
	}

	start := time.Now()
	err := h.handleFunc(ctx, evt, payload)
	elapsed := time.Since(start)

	// 记录指标
	if h.metrics != nil {
		eventType := evt.GetType()
		success := "true"
		if err != nil {
			success = "false"
		}

		labels := observe.MetricLabels{
			"handler":    h.handlerName,
			"event_type": eventType,
			"success":    success,
		}

		h.metrics.Counter("event_handler_total", 1, labels)
		h.metrics.Histogram("event_handler_duration_ms", float64(elapsed.Milliseconds()), labels)
	}

	return err
}

func (h *TypedEventHandler[T]) EventTypes() []string {
	return h.eventTypes
}

func (h *TypedEventHandler[T]) HandlerName() string {
	return h.handlerName
}

// Type 返回处理器的稳定名称，供消息总线匹配与日志记录使用。
func (h *TypedEventHandler[T]) Type() string {
	return h.handlerName
}

// 编译期断言：确保 TypedEventHandler 满足 IEventHandler 接口。
var _ IEventHandler = (*TypedEventHandler[any])(nil)
