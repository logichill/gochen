// Package direct 提供一个同步的消息传输实现。
package direct

import (
	"context"
	"fmt"
	"gochen/contextx"
	"gochen/errors"
	"gochen/messaging"
	"gochen/messaging/transport/internal/subscriptions"
	"runtime/debug"
	"strings"
	"sync"
)

// SyncTransport 是一个同步的内存传输实现。
// 它的 Publish 方法会立即在同一个 goroutine 中调用所有匹配的处理器。
type SyncTransport struct {
	handlers map[string][]messaging.IMessageHandler
	mutex    sync.RWMutex
	running  bool
}

// NewSyncTransport 创建一个同步执行 handler 的内存传输实现。
func NewSyncTransport() *SyncTransport {
	return &SyncTransport{
		handlers: make(map[string][]messaging.IMessageHandler),
	}
}

// Publish 在当前 goroutine 内同步调用所有匹配的消息处理器。
func (t *SyncTransport) Publish(ctx context.Context, message messaging.IMessage) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if message == nil {
		return errors.NewCode(errors.InvalidInput, "message is nil")
	}
	if strings.TrimSpace(message.GetType()) == "" {
		return errors.NewCode(errors.InvalidInput, "message type is required")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	t.mutex.RLock()
	if !t.running {
		t.mutex.RUnlock()
		return errors.NewCode(errors.Conflict, "sync transport is not running")
	}

	messageType := message.GetType()
	exact := t.handlers[messageType]
	wildcard := t.handlers["*"]

	// 复制 handlers 切片，防止迭代时被修改导致竞态。
	combinedLen := len(exact) + len(wildcard)
	handlers := make([]messaging.IMessageHandler, 0, combinedLen)
	if len(exact) > 0 {
		handlers = append(handlers, exact...)
	}
	if len(wildcard) > 0 {
		handlers = append(handlers, wildcard...)
	}
	t.mutex.RUnlock()

	if len(handlers) == 0 {
		// 在同步模式下，没有处理器通常不是错误，只是无人监听
		return nil
	}

	// 默认贯通：将 metadata 中的链路信息（tenant/trace/operator）与 message.Metadata 双向补齐；
	// 并确保 trace_id 缺失时能用 message.ID 兜底，便于日志关联。
	derived := ctx
	if md := message.GetMetadata(); md != nil {
		var err error
		derived, err = contextx.DeriveFromMetadata(derived, md)
		if err != nil {
			return err
		}
		fallback := strings.TrimSpace(message.GetID())
		if fallback == "" {
			fallback = contextx.GenerateTraceID()
		}
		derived, err = contextx.EnsureTraceID(derived, md, fallback)
		if err != nil {
			return err
		}
		_ = contextx.InjectTenantID(derived, md)
		_ = contextx.InjectOperator(derived, md)
	}

	var errs []error
	for _, handler := range handlers {
		err := func() (err error) {
			defer func() {
				if r := recover(); r != nil {
					err = errors.NewCode(errors.Internal, "message handler panicked").
						WithContext("panic", fmt.Sprint(r)).
						WithContext("stack", string(debug.Stack())).
						WithContext("handler_type", handler.Type()).
						WithContext("message_type", messageType).
						WithContext("message_id", message.GetID())
				}
			}()
			return handler.Handle(derived, message)
		}()
		if err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		if len(errs) == 1 {
			return errs[0]
		}
		joined := errors.Join(errs...)
		return errors.Wrap(joined, errors.Internal, "message handling completed with multiple errors").
			WithContext("error_count", len(errs))
	}
	return nil
}

// PublishAll 按顺序同步发布一批消息，遇错立即停止。
func (t *SyncTransport) PublishAll(ctx context.Context, messages []messaging.IMessage) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	for i, message := range messages {
		if message == nil {
			return errors.NewCode(errors.InvalidInput, "message is nil").WithContext("index", i)
		}
		if err := t.Publish(ctx, message); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			if appErr, ok := err.(*errors.AppError); ok && appErr != nil {
				return appErr.WithContext("index", i).WithContext("message_id", message.GetID())
			}
			return errors.Wrap(err, errors.Internal, "failed to publish message").
				WithContext("index", i).
				WithContext("message_id", message.GetID())
		}
	}
	return nil
}

// Subscribe 为指定消息类型注册处理器，并返回取消订阅函数。
func (t *SyncTransport) Subscribe(ctx context.Context, messageType string, handler messaging.IMessageHandler) (messaging.UnsubscribeFunc, error) {
	return subscriptions.Subscribe(ctx, &t.mutex, t.handlers, messageType, handler)
}

// Start 将传输层切换到可发布状态。
func (t *SyncTransport) Start(ctx context.Context) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	t.mutex.Lock()
	defer t.mutex.Unlock()
	if t.running {
		return errors.NewCode(errors.Conflict, "sync transport is already running")
	}
	t.running = true
	return nil
}

// Stop 关闭传输层，之后不再接受新的发布请求。
func (t *SyncTransport) Stop(ctx context.Context) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}

	t.mutex.Lock()
	defer t.mutex.Unlock()
	if !t.running {
		return messaging.NewTransportAlreadyStoppedError("sync transport is not running")
	}
	t.running = false
	return nil
}

// StopWithSnapshot 以统一 shutdown 签名包装 Stop；同步传输不会留下待处理消息。
func (t *SyncTransport) StopWithSnapshot(ctx context.Context) ([]messaging.IMessage, error) {
	if err := t.Stop(ctx); err != nil {
		return nil, err
	}
	return nil, nil
}

func (t *SyncTransport) Stats() messaging.TransportStats {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	handlerCount := 0
	messageTypes := make([]string, 0, len(t.handlers))
	for mt, h := range t.handlers {
		messageTypes = append(messageTypes, mt)
		handlerCount += len(h)
	}

	return messaging.TransportStats{
		Running:      t.running,
		HandlerCount: handlerCount,
		MessageTypes: messageTypes,
	}
}

// IsSynchronous 标记该传输实现会在当前调用栈内同步执行 handler。
func (t *SyncTransport) IsSynchronous() bool { return true }
