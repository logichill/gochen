// Package direct 提供一个同步的消息传输实现。
package direct

import (
	"context"
	"sync"

	"gochen/messaging"
	"gochen/messaging/transport/internal/subscription"
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

// Subscribe 为指定消息类型注册处理器，并返回取消订阅函数。
func (t *SyncTransport) Subscribe(ctx context.Context, messageType string, handler messaging.IMessageHandler) (messaging.UnsubscribeFunc, error) {
	return subscription.Subscribe(ctx, &t.mutex, t.handlers, messageType, handler)
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
