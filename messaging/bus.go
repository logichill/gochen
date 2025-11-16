// Package core 提供消息总线的核心功能，包含基础的消息发布、订阅、路由和处理机制
package messaging

import (
	"context"
	"fmt"
	"sync"
)

// HandlerFunc 是一个函数类型，用于处理消息。它是中间件链中的基本执行单元
type HandlerFunc func(ctx context.Context, message IMessage) error

// IMiddleware 定义了消息总线中间件的接口
type IMiddleware interface {
	Handle(ctx context.Context, message IMessage, next HandlerFunc) error
	Name() string
}

// IMessageBus 消息总线接口
type IMessageBus interface {
	Subscribe(ctx context.Context, messageType string, handler IMessageHandler) error
	Unsubscribe(ctx context.Context, messageType string, handler IMessageHandler) error
	Publish(ctx context.Context, message IMessage) error
	PublishAll(ctx context.Context, messages []IMessage) error
	Use(middleware IMiddleware)
}

// MessageBus 消息总线基础实现
// 它依赖于 Transport 接口来处理实际的消息传输，并支持中间件
type MessageBus struct {
	transport   Transport
	middlewares []IMiddleware
	mutex       sync.RWMutex
}

// NewMessageBus 创建消息总线
func NewMessageBus(transport Transport) *MessageBus {
	return &MessageBus{
		transport:   transport,
		middlewares: make([]IMiddleware, 0),
	}
}

// Use 注册中间件
func (bus *MessageBus) Use(middleware IMiddleware) {
	bus.mutex.Lock()
	defer bus.mutex.Unlock()
	bus.middlewares = append(bus.middlewares, middleware)
}

// Subscribe 订阅消息处理器
func (bus *MessageBus) Subscribe(ctx context.Context, messageType string, handler IMessageHandler) error {
	return bus.transport.Subscribe(messageType, handler)
}

// Unsubscribe 取消订阅消息处理器
func (bus *MessageBus) Unsubscribe(ctx context.Context, messageType string, handler IMessageHandler) error {
	return bus.transport.Unsubscribe(messageType, handler)
}

// Publish 发布消息，并在发送到 Transport 前执行中间件
func (bus *MessageBus) Publish(ctx context.Context, message IMessage) error {
	finalHandler := func(ctx context.Context, msg IMessage) error {
		return bus.transport.Publish(ctx, msg)
	}
	return bus.executeMiddlewares(ctx, message, finalHandler)
}

// PublishAll 发布多个消息
func (bus *MessageBus) PublishAll(ctx context.Context, messages []IMessage) error {
	if len(messages) == 0 {
		return nil
	}

	batched := make([]IMessage, 0, len(messages))
	for _, message := range messages {
		err := bus.executeMiddlewares(ctx, message, func(ctx context.Context, msg IMessage) error {
			batched = append(batched, msg)
			return nil
		})
		if err != nil {
			return fmt.Errorf("failed to publish message %s: %w", message.GetID(), err)
		}
	}

	if len(batched) == 0 {
		return nil
	}

	if err := bus.transport.PublishAll(ctx, batched); err != nil {
		return fmt.Errorf("failed to publish batch (%d messages): %w", len(batched), err)
	}

	return nil
}

// executeMiddlewares 构建并执行中间件链
func (bus *MessageBus) executeMiddlewares(ctx context.Context, message IMessage, finalHandler HandlerFunc) error {
	bus.mutex.RLock()
	middlewares := bus.middlewares
	bus.mutex.RUnlock()

	if len(middlewares) == 0 {
		return finalHandler(ctx, message)
	}

	next := finalHandler
	for i := len(middlewares) - 1; i >= 0; i-- {
		middleware := middlewares[i]
		currentNext := next
		next = func(ctx context.Context, msg IMessage) error {
			return middleware.Handle(ctx, msg, currentNext)
		}
	}
	return next(ctx, message)
}
