// Package bus 提供事件总线的具体实现，它是一个围绕通用 MessageBus 的类型安全包装器
package bus

import (
	"context"
	"fmt"

	"gochen/eventing"
	"gochen/messaging"
)

// IEventHandler 事件处理器接口
type IEventHandler interface {
	messaging.IMessageHandler
	HandleEvent(ctx context.Context, evt eventing.IEvent) error
	GetEventTypes() []string
	GetHandlerName() string
}

// EventHandlerFunc 事件处理器函数类型
type EventHandlerFunc func(ctx context.Context, evt eventing.IEvent) error

func (f EventHandlerFunc) HandleEvent(ctx context.Context, evt eventing.IEvent) error {
	return f(ctx, evt)
}

func (f EventHandlerFunc) Handle(ctx context.Context, message messaging.IMessage) error {
	evt, ok := message.(eventing.IEvent)
	if !ok {
		return fmt.Errorf("message is not an event: %T", message)
	}
	return f(ctx, evt)
}

func (f EventHandlerFunc) GetEventTypes() []string { return []string{"*"} }
func (f EventHandlerFunc) GetHandlerName() string  { return "EventHandlerFunc" }
func (f EventHandlerFunc) Type() string            { return "*" }

// IEventBus 事件总线接口
type IEventBus interface {
	messaging.IMessageBus
	PublishEvent(ctx context.Context, evt eventing.IEvent) error
	PublishEvents(ctx context.Context, events []eventing.IEvent) error
	SubscribeEvent(ctx context.Context, eventType string, handler IEventHandler) error
	UnsubscribeEvent(ctx context.Context, eventType string, handler IEventHandler) error
	// 便捷方法：按处理器声明的事件类型批量订阅/取消订阅
	SubscribeHandler(ctx context.Context, handler IEventHandler) error
	UnsubscribeHandler(ctx context.Context, handler IEventHandler) error
}

// EventBus 是消息总线的类型安全包装器
type EventBus struct {
	messaging.IMessageBus
}

// NewEventBus 创建新的事件总线实例
func NewEventBus(messageBus messaging.IMessageBus) *EventBus {
	return &EventBus{
		IMessageBus: messageBus,
	}
}

// PublishEvent 发布单个事件
func (eb *EventBus) PublishEvent(ctx context.Context, evt eventing.IEvent) error {
	return eb.IMessageBus.Publish(ctx, evt)
}

// PublishEvents 发布多个事件
func (eb *EventBus) PublishEvents(ctx context.Context, events []eventing.IEvent) error {
	messages := make([]messaging.IMessage, len(events))
	for i, e := range events {
		messages[i] = e
	}
	return eb.IMessageBus.PublishAll(ctx, messages)
}

// SubscribeEvent 订阅事件处理器
func (eb *EventBus) SubscribeEvent(ctx context.Context, eventType string, handler IEventHandler) error {
	return eb.IMessageBus.Subscribe(ctx, eventType, handler)
}

// UnsubscribeEvent 取消订阅事件处理器
func (eb *EventBus) UnsubscribeEvent(ctx context.Context, eventType string, handler IEventHandler) error {
	return eb.IMessageBus.Unsubscribe(ctx, eventType, handler)
}

// SubscribeHandler 按处理器声明的事件类型批量订阅
func (eb *EventBus) SubscribeHandler(ctx context.Context, handler IEventHandler) error {
	types := handler.GetEventTypes()
	if len(types) == 0 {
		types = []string{"*"}
	}
	for _, t := range types {
		if err := eb.SubscribeEvent(ctx, t, handler); err != nil {
			return err
		}
	}
	return nil
}

// UnsubscribeHandler 按处理器声明的事件类型批量取消订阅
func (eb *EventBus) UnsubscribeHandler(ctx context.Context, handler IEventHandler) error {
	types := handler.GetEventTypes()
	if len(types) == 0 {
		types = []string{"*"}
	}
	for _, t := range types {
		if err := eb.UnsubscribeEvent(ctx, t, handler); err != nil {
			return err
		}
	}
	return nil
}
