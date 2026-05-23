// Package deadletter 提供 DLQ（死信）记录的公共抽象。
package deadletter

import (
	"context"
	"time"

	"gochen/messaging"
)

// Entry 表示一次消息处理失败后的“死信”记录。
//
// 注意：不同 Transport 的失败语义可能不同：
//   - 同步 Transport：失败通常会向上游返回 error；
//   - 异步 Transport（如 MemoryTransport）：失败不会传播给发布者，需要通过日志/钩子/DLQ 收敛。
type Entry struct {
	// Message 为原始消息（只读视图），用于诊断或后续人工补偿。
	Message messaging.IMessage

	// HandlerType 为触发错误的处理器类型（handler.Type()）。
	HandlerType string

	// Err 为处理失败的原因。
	Err error

	// OccurredAt 为记录发生时间。
	OccurredAt time.Time
}

// ISink 为 DLQ（死信）写入抽象。
//
// 典型实现：
//   - 内存记录（测试/开发）
//   - SQL 表（生产）
//   - 外部队列（Kafka/NATS/Redis Streams 等）
type ISink interface {
	Write(ctx context.Context, entry Entry) error
}
