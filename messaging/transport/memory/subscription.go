// Package memory 实现订阅管理
package memory

import (
	"fmt"

	"gochen/messaging"
)

// Subscribe 订阅消息处理器
//
// 支持多个处理器订阅同一消息类型
// 支持通配符 "*" 订阅所有消息
//
// 参数:
//   - messageType: 消息类型（支持 "*" 通配符）
//   - handler: 消息处理器
//
// 返回:
//   - error: 订阅失败时返回错误
func (t *MemoryTransport) Subscribe(messageType string, handler messaging.IMessageHandler) error {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	if t.handlers[messageType] == nil {
		t.handlers[messageType] = make([]messaging.IMessageHandler, 0)
	}

	t.handlers[messageType] = append(t.handlers[messageType], handler)
	return nil
}

// Unsubscribe 取消订阅消息处理器
//
// 参数:
//   - messageType: 消息类型
//   - handler: 待移除的处理器
//
// 返回:
//   - error: 处理器不存在时返回错误
func (t *MemoryTransport) Unsubscribe(messageType string, handler messaging.IMessageHandler) error {
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
