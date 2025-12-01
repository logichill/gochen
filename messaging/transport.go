// Package core 提供消息传输层抽象
package messaging

import (
	"context"
)

// ITransport 消息传输接口
//
// 语义约定：
//   - Publish/PublishAll 返回的 error 只代表“传输层本身”的错误（连接失败、队列已满、未 Start 等）；
//   - 对于异步实现（如 memory/redisstreams/natsjetstream），消息处理器（IMessageHandler.Handle）的错误通常不会通过返回值暴露，
//     而是由实现自行记录日志或上报监控；
//   - 对于同步实现（如 transport/sync），Publish/PublishAll 可能会在同一调用中直接执行所有处理器，并将其错误聚合到返回值中。
//
// 调用方应将非 nil error 视为“消息未成功交给传输层”的信号；业务级错误建议通过消息载荷或领域层约定返回，而不是依赖 Transport 的 error。
type ITransport interface {
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
