// Package memory 实现消息分发逻辑
package memory

import (
	"context"

	"gochen/messaging"
)

// dispatch 分发消息到订阅的处理器
//
// 处理流程:
//  1. 获取精确匹配的处理器
//  2. 获取通配符处理器 ("*")
//  3. 并发调用所有处理器
//  4. 记录处理结果
//
// 参数:
//   - ctx: 上下文
//   - message: 待分发的消息
func (t *MemoryTransport) dispatch(ctx context.Context, message messaging.IMessage) {
	t.mutex.RLock()
	messageType := message.GetType()

	// 收集精确匹配和通配符("*")的处理器
	exact := t.handlers[messageType]
	wildcard := t.handlers["*"]

	// 拷贝到新的切片，避免在读锁释放后被并发修改
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
		return
	}

	// 调用所有注册的处理器
	for _, handler := range handlers {
		// 忽略错误，继续处理其他处理器
		_ = handler.Handle(ctx, message)
	}
}
