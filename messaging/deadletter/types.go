// Package deadletter 提供消息处理器失败记录的 sink 抽象。
//
// 本包位于消息投递之后：Transport 已把消息交给 handler，但 handler 返回错误或 panic 时，Entry
// 用于记录诊断与人工补偿信息。它不管理发布状态、claim、自动重试或重投。
//
// 持久化 Outbox 在“事件尚未成功发布”阶段耗尽重试时，应使用 eventing/outbox.IDLQRepository；
// 后者保留 Outbox 行状态并提供查询、删除和重投能力。两者处于不同失败阶段，不应互相替代。
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

// ISink 为消息 handler 失败记录的写入抽象。
//
// 典型实现：
//   - 内存记录（测试/开发）
//   - SQL 表（生产）
//   - 外部队列（Kafka/NATS/Redis Streams 等）
//
// ISink 只接收失败快照，不承诺持久重试或重投；需要 Outbox 发布失败恢复时使用
// eventing/outbox.IDLQRepository。
type ISink interface {
	Write(ctx context.Context, entry Entry) error
}
