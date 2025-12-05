// Package sync 提供一个同步的消息传输实现
package sync

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"gochen/messaging"
)

// SyncTransport 是一个同步的内存传输实现
// 它的 Publish 方法会立即在同一个 goroutine 中调用所有匹配的处理器
type SyncTransport struct {
	handlers map[string][]messaging.IMessageHandler
	mutex    sync.RWMutex
	running  bool
}

// NewSyncTransport 创建一个新的同步传输实例
func NewSyncTransport() *SyncTransport {
	return &SyncTransport{
		handlers: make(map[string][]messaging.IMessageHandler),
	}
}

// Publish 立即、同步地发布消息
func (t *SyncTransport) Publish(ctx context.Context, message messaging.IMessage) error {
	t.mutex.RLock()
	if !t.running {
		t.mutex.RUnlock()
		return fmt.Errorf("sync transport is not running")
	}

	messageType := message.GetType()
	handlers := t.handlers[messageType]
	t.mutex.RUnlock()

	if len(handlers) == 0 {
		// 在同步模式下，没有处理器通常不是错误，只是无人监听
		return nil
	}

	var errs []error
	for _, handler := range handlers {
		if err := handler.Handle(ctx, message); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		joined := errors.Join(errs...)
		return fmt.Errorf("message handling completed with %d errors: %w", len(errs), joined)
	}
	return nil
}

// PublishAll 批量发布消息（同步执行）
func (t *SyncTransport) PublishAll(ctx context.Context, messages []messaging.IMessage) error {
	for _, message := range messages {
		if err := t.Publish(ctx, message); err != nil {
			return fmt.Errorf("failed to publish message %s: %w", message.GetID(), err)
		}
	}
	return nil
}

// Subscribe 订阅消息处理器
func (t *SyncTransport) Subscribe(messageType string, handler messaging.IMessageHandler) error {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	if t.handlers[messageType] == nil {
		t.handlers[messageType] = make([]messaging.IMessageHandler, 0)
	}
	t.handlers[messageType] = append(t.handlers[messageType], handler)
	return nil
}

// Unsubscribe 取消订阅消息处理器
func (t *SyncTransport) Unsubscribe(messageType string, handler messaging.IMessageHandler) error {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	handlers, ok := t.handlers[messageType]
	if !ok {
		return fmt.Errorf("no handlers for message type %s", messageType)
	}

	for i, h := range handlers {
		if h == handler {
			t.handlers[messageType] = append(handlers[:i], handlers[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("handler not found for message type %s", messageType)
}

// Start 启动传输层
func (t *SyncTransport) Start(ctx context.Context) error {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	if t.running {
		return fmt.Errorf("sync transport is already running")
	}
	t.running = true
	return nil
}

// Close 关闭传输层
func (t *SyncTransport) Close() error {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	if !t.running {
		return fmt.Errorf("sync transport is not running")
	}
	t.running = false
	return nil
}

// Stats 返回统计信息
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
