package messaging

import (
	"context"
	"fmt"
	"reflect"
	"runtime/debug"

	"gochen/contextx"
	"gochen/errors"
	"gochen/internal/cleanup"
)

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
// 退订失败时保留重试能力；并发退订串行执行，等待时响应 context 取消。
func (bus *MessageBus) Subscribe(ctx context.Context, messageType string, handler IMessageHandler) (UnsubscribeFunc, error) {
	if bus == nil || bus.transport == nil {
		return nil, errors.NewCode(errors.InvalidInput, "message bus transport cannot be nil")
	}
	if ctx == nil {
		return nil, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if messageType == "" {
		return nil, errors.NewCode(errors.InvalidInput, "message type cannot be empty")
	}
	if handler == nil || isNilMessageHandler(handler) {
		return nil, errors.NewCode(errors.InvalidInput, "handler cannot be nil")
	}

	// 每次 Subscribe 都创建一个独立的包装器与订阅实例，避免对 handler identity 的依赖。
	wrapped := &handlerWithErrorHook{inner: handler, bus: bus}
	unsub, err := bus.transport.Subscribe(ctx, messageType, wrapped)
	if err != nil {
		return nil, err
	}

	return cleanup.Retryable(unsub), nil
}

func isNilMessageHandler(handler IMessageHandler) bool {
	return isNilValue(reflect.ValueOf(handler))
}

// handlerWithErrorHook 包装 IMessageHandler，在处理器返回错误时触发 MessageBus 的错误钩子。
type handlerWithErrorHook struct {
	inner IMessageHandler
	bus   *MessageBus
}

// Handle 在真正调用底层 handler 前补齐链路上下文，并在出错时触发错误钩子。
func (h *handlerWithErrorHook) Handle(ctx context.Context, message IMessage) (err error) {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	defer func() {
		messageType := ""
		messageID := ""
		if message != nil {
			messageType = message.GetType()
			messageID = message.GetID()
		}
		if r := recover(); r != nil {
			err = errors.NewCode(errors.Internal, "message handler panicked").
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

	if message != nil && !IsNilMessage(message) {
		// typed-nil 指针（如 (*Message)(nil)）实现接口后 != nil，但调用
		// GetMetadata() 会 panic；与发布侧中间件一致用 IsNilMessage 统一判定。
		// 若 Transport 未透传上游 ctx（典型跨进程/异步场景），从 metadata 派生回链路语义；
		// 同时补齐 trace_id：当上游未携带 trace_id 时，用 message.ID 兜底，保证消费侧日志/错误钩子可关联。
		// 用临时变量接住派生失败：直接覆盖 ctx 会让 defer 中的 hook(ctx, ...) 拿到 nil context 并 panic。
		md := message.GetMetadata()
		derivedCtx, derr := contextx.DeriveFromMetadata(ctx, md)
		if derr != nil {
			return derr
		}
		ctx = derivedCtx
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
