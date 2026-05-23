// Package bus 提供事件总线的具体实现，它是一个围绕通用 MessageBus 的类型安全包装器。
package bus

import (
	"context"
	"fmt"
	"sync"

	"gochen/errors"
	"gochen/eventing"
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
	return f(ctx, evt)
}

// Handle 让事件处理器兼容消息总线需要的通用 IMessageHandler 接口。
func (f EventHandlerFunc) Handle(ctx context.Context, message messaging.IMessage) error {
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

// IEventBus 事件总线接口。
type IEventBus interface {
	messaging.IMessageBus
	PublishEvent(ctx context.Context, evt eventing.IEvent) error
	PublishEvents(ctx context.Context, events []eventing.IEvent) error
	SubscribeEvent(ctx context.Context, eventType string, handler IEventHandler) (messaging.UnsubscribeFunc, error)
	// 便捷方法：按处理器声明的事件类型批量订阅，并返回一个“一次性取消所有订阅”的函数。
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
	return eb.IMessageBus.Publish(ctx, evt)
}

// PublishEvents 批量发布多个事件。
func (eb *EventBus) PublishEvents(ctx context.Context, events []eventing.IEvent) error {
	messages := make([]messaging.IMessage, len(events))
	for i, e := range events {
		messages[i] = e
	}
	return eb.IMessageBus.PublishAll(ctx, messages)
}

// SubscribeEvent 订阅指定类型事件并注册处理器。
func (eb *EventBus) SubscribeEvent(ctx context.Context, eventType string, handler IEventHandler) (messaging.UnsubscribeFunc, error) {
	return eb.IMessageBus.Subscribe(ctx, eventType, handler)
}

// SubscribeHandler 按处理器声明的事件类型批量订阅。
func (eb *EventBus) SubscribeHandler(ctx context.Context, handler IEventHandler) (messaging.UnsubscribeFunc, error) {
	if ctx == nil {
		return nil, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	types := handler.EventTypes()
	if len(types) == 0 {
		types = []string{"*"}
	}

	unsubs := make([]messaging.UnsubscribeFunc, 0, len(types))
	for _, t := range types {
		unsub, err := eb.SubscribeEvent(ctx, t, handler)
		if err != nil {
			// 回滚已创建订阅，避免半注册状态。
			for i := len(unsubs) - 1; i >= 0; i-- {
				_ = unsubs[i](ctx)
			}
			return nil, err
		}
		unsubs = append(unsubs, unsub)
	}

	var once sync.Once
	return func(unsubCtx context.Context) error {
		if unsubCtx == nil {
			return errors.NewCode(errors.InvalidInput, "ctx is nil")
		}
		var firstErr error
		once.Do(func() {
			for i := len(unsubs) - 1; i >= 0; i-- {
				if err := unsubs[i](unsubCtx); err != nil && firstErr == nil {
					firstErr = err
				}
			}
		})
		return firstErr
	}, nil
}
