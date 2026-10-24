package bus

import (
	"context"
	"fmt"
	"reflect"

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

func isNilEventHandler(handler IEventHandler) bool {
	// 纯字面量 nil：reflect.ValueOf(nil).Kind() == Invalid，需显式短路，
	// 否则进入 default 分支误判为“非空”。
	if handler == nil {
		return true
	}
	value := reflect.ValueOf(handler)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
