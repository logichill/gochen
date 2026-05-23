// Package memory 实现订阅管理。
package memory

import (
	"context"

	"gochen/messaging"
	"gochen/messaging/transport/internal/subscriptions"
)

// Subscribe 订阅消息并注册处理器。
//
// 说明：
// - Subscribe 订阅消息处理器。
// - 支持多个处理器订阅同一消息类型。
// - 支持通配符 "*" 订阅所有消息。
// - 参数:
// - - messageType: 消息类型（支持 "*" 通配符）
// - - handler: 消息处理器。
// - 返回:
// - - messaging.UnsubscribeFunc: 用于取消订阅的函数（幂等）
// - - error: 订阅失败时返回错误。
func (t *MemoryTransport) Subscribe(ctx context.Context, messageType string, handler messaging.IMessageHandler) (messaging.UnsubscribeFunc, error) {
	// Keep ctx nil checks and other validations consistent across transports.
	return subscriptions.Subscribe(ctx, &t.mutex, t.handlers, messageType, handler)
}
