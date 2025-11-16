// Package core 提供消息处理器抽象
package messaging

import (
	"context"
)

// IMessageHandler 消息处理器接口
type IMessageHandler interface {
	// Handle 处理消息
	Handle(ctx context.Context, message IMessage) error

	// Type 返回处理器类型（用于日志和调试）
	Type() string
}

// MessageResult 消息处理结果
type MessageResult struct {
	Success   bool                   `json:"success"`
	MessageID string                 `json:"message_id"`
	Result    interface{}            `json:"result,omitempty"`
	Error     error                  `json:"error,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	Duration  int64                  `json:"duration"` // 执行时间（毫秒）
}
