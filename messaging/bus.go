// Package messaging 提供消息总线的核心功能，包含基础的消息发布、订阅、路由和处理机制。
package messaging

import (
	"context"
	"fmt"
	"gochen/contextx"
	gerrors "gochen/errors"
	"runtime/debug"
	"sync"
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

// MessageBus 消息总线基础实现。
// 它依赖于 ITransport 接口来处理实际的消息传输，并支持中间件。
type MessageBus struct {
	transport   ITransport
	middlewares []IMiddleware
	mutex       sync.RWMutex

	// handlerErrorHook 可选的处理器错误钩子
	handlerErrorHook HandlerErrorHook
}

// NewMessageBus 创建一个基于指定 transport 的消息总线。
func NewMessageBus(transport ITransport) *MessageBus {
	return &MessageBus{
		transport:   transport,
		middlewares: make([]IMiddleware, 0),
	}
}

// Transport 返回底层传输实现。
//
// 该方法主要给装配或桥接代码使用；业务层应尽量只依赖 IMessageBus 抽象。
func (bus *MessageBus) Transport() ITransport {
	return bus.transport
}

// Use 追加一层消息总线中间件。
func (bus *MessageBus) Use(middleware IMiddleware) {
	bus.mutex.Lock()
	defer bus.mutex.Unlock()
	bus.middlewares = append(bus.middlewares, middleware)
}

// SetHandlerErrorHook 设置处理器错误监控钩子。
func (bus *MessageBus) SetHandlerErrorHook(h HandlerErrorHook) {
	bus.mutex.Lock()
	defer bus.mutex.Unlock()
	bus.handlerErrorHook = h
}

// getHandlerErrorHook 并发安全地读取当前错误钩子。
func (bus *MessageBus) getHandlerErrorHook() HandlerErrorHook {
	bus.mutex.RLock()
	defer bus.mutex.RUnlock()
	return bus.handlerErrorHook
}

// Subscribe 为指定消息类型注册处理器，并返回幂等的取消订阅函数。
func (bus *MessageBus) Subscribe(ctx context.Context, messageType string, handler IMessageHandler) (UnsubscribeFunc, error) {
	if bus == nil || bus.transport == nil {
		return nil, gerrors.NewCode(gerrors.InvalidInput, "message bus transport cannot be nil")
	}
	if ctx == nil {
		return nil, gerrors.NewCode(gerrors.InvalidInput, "ctx is nil")
	}
	if messageType == "" {
		return nil, gerrors.NewCode(gerrors.InvalidInput, "message type cannot be empty")
	}
	if handler == nil {
		return nil, gerrors.NewCode(gerrors.InvalidInput, "handler cannot be nil")
	}

	// 每次 Subscribe 都创建一个独立的包装器与订阅实例，避免对 handler identity 的依赖。
	wrapped := &handlerWithErrorHook{inner: handler, bus: bus}
	unsub, err := bus.transport.Subscribe(ctx, messageType, wrapped)
	if err != nil {
		return nil, err
	}

	var once sync.Once
	return func(unsubCtx context.Context) error {
		if unsubCtx == nil {
			return gerrors.NewCode(gerrors.InvalidInput, "ctx is nil")
		}
		var err error
		once.Do(func() {
			err = unsub(unsubCtx)
		})
		return err
	}, nil
}

// Publish 发布单条消息，并在真正投递前执行中间件链。
func (bus *MessageBus) Publish(ctx context.Context, message IMessage) error {
	if bus == nil {
		return gerrors.NewCode(gerrors.InvalidInput, "bus is nil")
	}
	if bus.transport == nil {
		return gerrors.NewCode(gerrors.InvalidInput, "transport is nil")
	}
	if ctx == nil {
		return gerrors.NewCode(gerrors.InvalidInput, "ctx is nil")
	}
	if message == nil {
		return gerrors.NewCode(gerrors.InvalidInput, "message is nil")
	}
	finalHandler := func(ctx context.Context, msg IMessage) error {
		return bus.transport.Publish(ctx, msg)
	}
	return bus.executeMiddlewares(ctx, message, finalHandler)
}

// PublishAll 批量发布消息，并确保每条消息都先经过中间件链。
func (bus *MessageBus) PublishAll(ctx context.Context, messages []IMessage) error {
	if len(messages) == 0 {
		return nil
	}
	if bus == nil {
		return gerrors.NewCode(gerrors.InvalidInput, "bus is nil")
	}
	if bus.transport == nil {
		return gerrors.NewCode(gerrors.InvalidInput, "transport is nil")
	}
	if ctx == nil {
		return gerrors.NewCode(gerrors.InvalidInput, "ctx is nil")
	}

	batched := make([]IMessage, 0, len(messages))
	for i, message := range messages {
		if message == nil {
			return gerrors.NewCode(gerrors.InvalidInput, "message is nil").WithContext("index", i)
		}
		err := bus.executeMiddlewares(ctx, message, func(ctx context.Context, msg IMessage) error {
			if msg == nil {
				return gerrors.NewCode(gerrors.InvalidInput, "middleware produced nil message").WithContext("index", i)
			}
			batched = append(batched, msg)
			return nil
		})
		if err != nil {
			var appErr *gerrors.AppError
			if gerrors.As(err, &appErr) && appErr != nil {
				return appErr.WithContext("message_id", message.GetID())
			}
			return gerrors.Wrap(err, gerrors.Internal, "failed to publish message").
				WithContext("message_id", message.GetID())
		}
	}

	if len(batched) == 0 {
		return nil
	}

	if err := bus.transport.PublishAll(ctx, batched); err != nil {
		return gerrors.Wrap(err, gerrors.Dependency, "failed to publish batch").
			WithContext("message_count", len(batched))
	}

	return nil
}

// executeMiddlewares 构建并执行最终处理链。
func (bus *MessageBus) executeMiddlewares(ctx context.Context, message IMessage, finalHandler HandlerFunc) error {
	if ctx == nil {
		return gerrors.NewCode(gerrors.InvalidInput, "ctx is nil")
	}

	// 默认贯通：将 metadata 中的链路信息（tenant/trace/operator）与 message.Metadata 双向补齐。
	// 说明：
	// - Publish：确保 metadata 携带关键字段，跨进程可关联；
	// - Consume：若 Transport 未透传 ctx，该信息也可从 metadata 派生回来（见 handlerWithErrorHook.Handle）。
	// 优先级约定：
	// - ctx 优先：同进程内不允许 metadata 覆盖 ctx（避免链路排障混乱）；
	// - metadata 仅用于“ctx 缺失时补齐”（典型：跨进程/异步 Transport 未透传 ctx）。
	if message != nil {
		md := message.GetMetadata()
		var err error
		ctx, err = contextx.DeriveFromMetadata(ctx, md)
		if err != nil {
			return err
		}
		ctx, err = contextx.EnsureTraceID(ctx, md, message.GetID())
		if err != nil {
			return err
		}
		if err := contextx.InjectTenantID(ctx, md); err != nil {
			return err
		}
		if err := contextx.InjectOperator(ctx, md); err != nil {
			return err
		}
	}

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

// handlerWithErrorHook 包装 IMessageHandler，在处理器返回错误时触发 MessageBus 的错误钩子。
type handlerWithErrorHook struct {
	inner IMessageHandler
	bus   *MessageBus
}

// Handle 在真正调用底层 handler 前补齐链路上下文，并在出错时触发错误钩子。
func (h *handlerWithErrorHook) Handle(ctx context.Context, message IMessage) (err error) {
	if ctx == nil {
		return gerrors.NewCode(gerrors.InvalidInput, "ctx is nil")
	}
	defer func() {
		messageType := ""
		messageID := ""
		if message != nil {
			messageType = message.GetType()
			messageID = message.GetID()
		}
		if r := recover(); r != nil {
			err = gerrors.NewCode(gerrors.Internal, "message handler panicked").
				WithContext("panic", fmt.Sprint(r)).
				WithContext("stack", string(debug.Stack())).
				WithContext("handler_type", h.inner.Type()).
				WithContext("message_type", messageType).
				WithContext("message_id", messageID)
		}
		if err != nil {
			if hook := h.bus.getHandlerErrorHook(); hook != nil {
				hook(ctx, message, err)
			}
		}
	}()

	if message != nil {
		// 若 Transport 未透传上游 ctx（典型的跨进程/异步场景），这里从 metadata 派生回链路语义；
		// 同时补齐 trace_id：当上游未携带 trace_id 时，用 message.ID 兜底，保证消费侧日志/错误钩子可关联。
		md := message.GetMetadata()
		ctx, err = contextx.DeriveFromMetadata(ctx, md)
		if err != nil {
			return err
		}
		ctx, err = contextx.EnsureTraceID(ctx, md, message.GetID())
		if err != nil {
			return err
		}
	}

	err = h.inner.Handle(ctx, message)
	return err
}

// Type 透传底层处理器声明的消息类型。
func (h *handlerWithErrorHook) Type() string {
	return h.inner.Type()
}
