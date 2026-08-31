// Package messaging 提供消息处理器抽象。
package messaging

import "context"

// IMessageHandler 消息处理器接口。
type IMessageHandler interface {
	Handle(ctx context.Context, message IMessage) error
	Type() string
}

// MessageResult 消息处理结果。
type MessageResult struct {
	Success   bool           `json:"success"`
	MessageID string         `json:"message_id"`
	Result    any            `json:"result,omitempty"`
	Error     error          `json:"error,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	Duration  int64          `json:"duration"` // 执行时间（毫秒）
}
