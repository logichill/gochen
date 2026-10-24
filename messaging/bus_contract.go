package messaging

import (
	"context"
)

// HandlerFunc 是一个函数类型，用于处理消息（中间件链中的基本执行单元）。
type HandlerFunc func(ctx context.Context, message IMessage) error

// HandlerErrorHook 是处理器错误监控钩子。
//
// 当 IMessageHandler 返回非 nil error 时，MessageBus 会调用该钩子；
// 默认不设置，调用方可按需注入统一的错误收集逻辑（日志/监控等）。
type HandlerErrorHook func(ctx context.Context, message IMessage, err error)

// IMiddleware 定义了消息总线中间件的接口。
type IMiddleware interface {
	Handle(ctx context.Context, message IMessage, next HandlerFunc) error
	Name() string
}

// IMessageBus 消息总线接口。
type IMessageBus interface {
	Subscribe(ctx context.Context, messageType string, handler IMessageHandler) (UnsubscribeFunc, error)
	Publish(ctx context.Context, message IMessage) error
	PublishAll(ctx context.Context, messages []IMessage) error
	Use(middleware IMiddleware)
}
