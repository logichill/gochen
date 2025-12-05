// Package core 提供消息总线的核心功能，包含基础的消息发布、订阅、路由和处理机制
package messaging

import (
	"context"
	"fmt"
	"sync"
)

// HandlerFunc 是一个函数类型，用于处理消息。它是中间件链中的基本执行单元
type HandlerFunc func(ctx context.Context, message IMessage) error

// HandlerErrorHook 是处理器错误监控钩子
//
// 当 IMessageHandler 返回非 nil error 时，MessageBus 会调用该钩子；
// 默认不设置，调用方可按需注入统一的错误收集逻辑（日志/监控等）。
type HandlerErrorHook func(ctx context.Context, message IMessage, err error)

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
// 它依赖于 ITransport 接口来处理实际的消息传输，并支持中间件
type MessageBus struct {
	transport   ITransport
	middlewares []IMiddleware
	mutex       sync.RWMutex

	// handlerErrorHook 可选的处理器错误钩子
	handlerErrorHook HandlerErrorHook

	// wrappedHandlers 映射 handler 标识 -> 包装后的 handler（用于 Unsubscribe）
	// 注意：
	//   - 不能直接以 handler 值作为 map key，因为某些实现类型（例如函数类型 EventHandlerFunc）在 Go 中不可比较，
	//     使用它们作为 map key 会在运行时触发 "hash of unhashable type" panic。
	//   - 也不能只用 handler 的类型/Type() 作为 key，否则同一 messageType 下的多个“同类型 handler 实例”
	//     会被错误折叠为同一个 key，导致后注册的 handler 无法被正确区分与解绑。
	//   - 这里通过组合 messageType + handler 实例指针（%p）+ handler.Type() 构造字符串 key，尽量在不引入反射的前提下区分不同实例。
	wrappedHandlers map[string]IMessageHandler
}

// NewMessageBus 创建消息总线
func NewMessageBus(transport ITransport) *MessageBus {
	return &MessageBus{
		transport:       transport,
		middlewares:     make([]IMiddleware, 0),
		wrappedHandlers: make(map[string]IMessageHandler),
	}
}

// GetTransport 返回底层传输实现
//
// 该方法主要用于内部组件（例如 CommandBus、SagaOrchestrator）在运行时探测
// ITransport 的同步/异步语义，从而给出更明确的错误语义说明或告警。普通调用方
// 不应依赖具体 Transport 类型来实现业务逻辑。
func (bus *MessageBus) GetTransport() ITransport {
	return bus.transport
}

// Use 注册中间件
func (bus *MessageBus) Use(middleware IMiddleware) {
	bus.mutex.Lock()
	defer bus.mutex.Unlock()
	bus.middlewares = append(bus.middlewares, middleware)
}

// SetHandlerErrorHook 设置可选的处理器错误监控钩子
func (bus *MessageBus) SetHandlerErrorHook(h HandlerErrorHook) {
	bus.mutex.Lock()
	defer bus.mutex.Unlock()
	bus.handlerErrorHook = h
}

// getHandlerErrorHook 获取当前错误钩子（并发安全）
func (bus *MessageBus) getHandlerErrorHook() HandlerErrorHook {
	bus.mutex.RLock()
	defer bus.mutex.RUnlock()
	return bus.handlerErrorHook
}

// Subscribe 订阅消息处理器
func (bus *MessageBus) Subscribe(ctx context.Context, messageType string, handler IMessageHandler) error {
	// 缩小锁范围：仅在访问 wrappedHandlers 时持锁，避免 transport.Subscribe 阻塞其他操作
	wrapped := bus.getOrCreateWrappedHandler(messageType, handler)
	return bus.transport.Subscribe(messageType, wrapped)
}

// getOrCreateWrappedHandler 获取或创建包装后的 handler（并发安全）
func (bus *MessageBus) getOrCreateWrappedHandler(messageType string, handler IMessageHandler) IMessageHandler {
	key := bus.handlerKey(messageType, handler)

	bus.mutex.RLock()
	wrapped, exists := bus.wrappedHandlers[key]
	bus.mutex.RUnlock()

	if exists {
		return wrapped
	}

	bus.mutex.Lock()
	defer bus.mutex.Unlock()

	// 双重检查，避免重复创建
	if wrapped, exists = bus.wrappedHandlers[key]; exists {
		return wrapped
	}

	wrapped = &handlerWithErrorHook{
		inner: handler,
		bus:   bus,
	}
	bus.wrappedHandlers[key] = wrapped
	return wrapped
}

// Unsubscribe 取消订阅消息处理器
func (bus *MessageBus) Unsubscribe(ctx context.Context, messageType string, handler IMessageHandler) error {
	// 缩小锁范围：仅在访问 wrappedHandlers 时持锁，避免 transport.Unsubscribe 阻塞其他操作
	wrapped, exists := bus.removeWrappedHandler(messageType, handler)

	if exists {
		return bus.transport.Unsubscribe(messageType, wrapped)
	}

	// 如果找不到包装器，回退到原始 handler（兼容直接订阅 Transport 场景）
	return bus.transport.Unsubscribe(messageType, handler)
}

// removeWrappedHandler 移除并返回包装后的 handler（并发安全）
func (bus *MessageBus) removeWrappedHandler(messageType string, handler IMessageHandler) (IMessageHandler, bool) {
	key := bus.handlerKey(messageType, handler)

	bus.mutex.Lock()
	defer bus.mutex.Unlock()

	wrapped, exists := bus.wrappedHandlers[key]
	if exists {
		delete(bus.wrappedHandlers, key)
	}
	return wrapped, exists
}

// handlerKey 构造用于 wrappedHandlers 映射的 key
// 组合 messageType、handler.Type() 与 handler 的动态类型字符串，尽量保证唯一且稳定。
func (bus *MessageBus) handlerKey(messageType string, handler IMessageHandler) string {
	if handler == nil {
		return messageType + "|<nil>|"
	}
	// 使用 handler 的实例指针（%p）作为近似 identity，再附带类型信息和声明的 Type() 便于调试
	return fmt.Sprintf("%s|%p|%s", messageType, handler, handler.Type())
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
	// 拷贝一份中间件切片，避免在执行链过程中受到后续 Use 调用的影响。
	middlewares := append([]IMiddleware(nil), bus.middlewares...)
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

// handlerWithErrorHook 包装 IMessageHandler，在处理器返回错误时触发 MessageBus 的错误钩子
type handlerWithErrorHook struct {
	inner IMessageHandler
	bus   *MessageBus
}

func (h *handlerWithErrorHook) Handle(ctx context.Context, message IMessage) error {
	err := h.inner.Handle(ctx, message)
	if err != nil {
		if hook := h.bus.getHandlerErrorHook(); hook != nil {
			hook(ctx, message, err)
		}
	}
	return err
}

func (h *handlerWithErrorHook) Type() string {
	return h.inner.Type()
}
