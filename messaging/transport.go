// Package core 提供消息传输层抽象
package messaging

import (
	"context"
)

// Transport 消息传输接口
type Transport interface {
	Publish(ctx context.Context, message IMessage) error
	PublishAll(ctx context.Context, messages []IMessage) error
	Subscribe(messageType string, handler IMessageHandler) error
	Unsubscribe(messageType string, handler IMessageHandler) error
	Start(ctx context.Context) error
	Close() error
	Stats() TransportStats
}

// TransportStats 传输层统计信息
type TransportStats struct {
	Running      bool     `json:"running"`
	HandlerCount int      `json:"handler_count"`
	MessageTypes []string `json:"message_types"`
	QueueSize    int      `json:"queue_size,omitempty"`
	QueueDepth   int      `json:"queue_depth,omitempty"`
	WorkerCount  int      `json:"worker_count,omitempty"`
}
