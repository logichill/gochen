// Package bus 提供事件总线的具体实现，它是一个围绕通用 MessageBus 的类型安全包装器。
package bus

import (
	"context"
	"fmt"
	"reflect"

	"gochen/errors"
	"gochen/eventing"
	"gochen/internal/cleanup"
	"gochen/messaging"
)

// IEventHandler 事件处理器接口。
type IEventHandler interface {
	messaging.IMessageHandler
	HandleEvent(ctx context.Context, evt eventing.IEvent) error
	EventTypes() []string
	HandlerName() string
}

// EventHandlerFunc 事件处理器函数类型。
type EventHandlerFunc func(ctx context.Context, evt eventing.IEvent) error

// HandleEvent 让函数适配为类型安全的事件处理器。
func (f EventHandlerFunc) HandleEvent(ctx context.Context, evt eventing.IEvent) error {
	if f == nil {
		return errors.NewCode(errors.InvalidInput, "event handler function is nil")
	}
	return f(ctx, evt)
}

// Handle 让事件处理器兼容消息总线需要的通用 IMessageHandler 接口。
func (f EventHandlerFunc) Handle(ctx context.Context, message messaging.IMessage) error {
	if f == nil {
		return errors.NewCode(errors.InvalidInput, "event handler function is nil")
	}
	evt, ok := message.(eventing.IEvent)
	if !ok {
		return errors.NewCode(errors.InvalidInput, "message is not an event").
			WithContext("message_type", fmt.Sprintf("%T", message))
	}
	return f(ctx, evt)
}

func (f EventHandlerFunc) EventTypes() []string { return []string{"*"} }

func (f EventHandlerFunc) HandlerName() string { return "EventHandlerFunc" }

func (f EventHandlerFunc) Type() string { return "*" }

func (eb *EventBus) validate() error {
	if eb == nil || eb.IMessageBus == nil {
		return errors.NewCode(errors.InvalidInput, "message bus cannot be nil")
	}
	return nil
}

// IEventBus 事件总线接口。
type IEventBus interface {
	messaging.IMessageBus
	PublishEvent(ctx context.Context, evt eventing.IEvent) error
	PublishEvents(ctx context.Context, events []eventing.IEvent) error
	SubscribeEvent(ctx context.Context, eventType string, handler IEventHandler) (messaging.UnsubscribeFunc, error)
	// SubscribeHandler 按事件类型批量订阅，返回可重试的聚合释放函数。
	// 注册失败且回滚未完成时，同时返回释放函数与错误，调用方必须保留并清理。
	SubscribeHandler(ctx context.Context, handler IEventHandler) (messaging.UnsubscribeFunc, error)
}

// EventBus 是消息总线的类型安全包装器。
//
// 并发语义：
//   - EventBus 本身不维护内部状态，所有并发安全由底层 IMessageBus 实现保证；
//   - 调用方可以在多 goroutine 中安全复用同一个 EventBus 实例，只要底层 IMessageBus 是并发安全的。
type EventBus struct {
	messaging.IMessageBus
}

// NewEventBus 基于通用消息总线创建一个类型安全的事件总线包装器。
func NewEventBus(messageBus messaging.IMessageBus) *EventBus {
	return &EventBus{
		IMessageBus: messageBus,
	}
}

// PublishEvent 发布单个事件。
func (eb *EventBus) PublishEvent(ctx context.Context, evt eventing.IEvent) error {
	if err := eb.validate(); err != nil {
		return err
	}
	if evt == nil {
		return errors.NewCode(errors.InvalidInput, "event cannot be nil")
	}
	return eb.IMessageBus.Publish(ctx, evt)
}

// PublishEvents 批量发布多个事件。
func (eb *EventBus) PublishEvents(ctx context.Context, events []eventing.IEvent) error {
	if err := eb.validate(); err != nil {
		return err
	}
	messages := make([]messaging.IMessage, len(events))
	for i, e := range events {
		if e == nil {
			return errors.NewCode(errors.InvalidInput, "event cannot be nil").
				WithContext("index", i)
		}
		messages[i] = e
	}
	return eb.IMessageBus.PublishAll(ctx, messages)
}

// SubscribeEvent 订阅指定类型事件并注册处理器。
func (eb *EventBus) SubscribeEvent(ctx context.Context, eventType string, handler IEventHandler) (messaging.UnsubscribeFunc, error) {
	if err := eb.validate(); err != nil {
		return nil, err
	}
	if handler == nil || isNilEventHandler(handler) {
		return nil, errors.NewCode(errors.InvalidInput, "event handler cannot be nil")
	}
	return eb.IMessageBus.Subscribe(ctx, eventType, handler)
}

// SubscribeHandler 按处理器声明的事件类型批量订阅。
// 释放逆序尝试全部订阅并聚合错误，成功项不再重复释放。
// 注册失败时自动回滚；回滚失败则返回剩余订阅的释放函数及完整错误链。
func (eb *EventBus) SubscribeHandler(ctx context.Context, handler IEventHandler) (messaging.UnsubscribeFunc, error) {
	if ctx == nil {
		return nil, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if err := eb.validate(); err != nil {
		return nil, err
	}
	if handler == nil || isNilEventHandler(handler) {
		return nil, errors.NewCode(errors.InvalidInput, "event handler cannot be nil")
	}
	types := handler.EventTypes()
	if len(types) == 0 {
		types = []string{"*"}
	}

	unsubs := make([]messaging.UnsubscribeFunc, 0, len(types))
	release := cleanup.Retryable(func(unsubCtx context.Context) error {
		var errs []error
		for i := len(unsubs) - 1; i >= 0; i-- {
			if unsubs[i] == nil {
				continue
			}
			if err := unsubs[i](unsubCtx); err != nil {
				errs = append(errs, err)
			} else {
				unsubs[i] = nil
			}
		}
		return errors.Join(errs...)
	})
	for _, t := range types {
		unsub, err := eb.SubscribeEvent(ctx, t, handler)
		if unsub != nil {
			unsubs = append(unsubs, unsub)
		}
		if err != nil {
			if len(unsubs) == 0 {
				return nil, err
			}
			if rollbackErr := release(ctx); rollbackErr != nil {
				return release, errors.Join(err, rollbackErr)
			}
			return nil, err
		}
	}

	return release, nil
}

func isNilEventHandler(handler IEventHandler) bool {
	value := reflect.ValueOf(handler)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
