// Package memory 实现消息分发逻辑
package memory

import (
	"context"

	"gochen/logging"
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
	messageType := message.GetType()

	t.mutex.RLock()
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
	// 注意：MemoryTransport 是异步分发，handler 错误不会传播给发布者。
	// 如需错误处理，请在 handler 内部实现重试/DLQ 等机制。
	for _, handler := range handlers {
		if err := handler.Handle(ctx, message); err != nil {
			// 记录错误但继续处理其他处理器
			t.logger.Warn(ctx, "message handler failed",
				logging.String("message_type", messageType),
				logging.String("message_id", message.GetID()),
				logging.Error(err))
		}
	}
}
