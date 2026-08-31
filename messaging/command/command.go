package command

import (
	"gochen/clock"
	"gochen/messaging"
)

// MetadataIdempotencyKey 是命令级幂等键的标准 metadata 字段。
const MetadataIdempotencyKey = "idempotency_key"

// Command 命令实现。
//
// Command 是 Message 的特化，用于表示系统中的写操作意图。
// 遵循 CQRS 模式，命令不返回结果（或仅返回成功/失败状态）。
//
// 设计原则：
//   - 命令是不可变的。
//   - 命令应该是幂等的（基于 ID）
//   - 命令包含执行所需的所有信息。
//   - 命令针对特定聚合根（通过 AggregateID 标识）
type Command struct {
	messaging.Message // 嵌入 Message，继承所有 IMessage 能力

	// AggregateID 目标聚合根 ID
	// 用于命令路由和并发控制
	AggregateID string `json:"aggregate_id"`

	// AggregateType 目标聚合类型
	// 例如："User", "Order", "Product"
	AggregateType string `json:"aggregate_type"`
}

// NewCommand 创建Command。
func NewCommand(id, commandType string, aggregateID string, aggregateType string, payload any) *Command {
	return NewCommandWithClock(nil, id, commandType, aggregateID, aggregateType, payload)
}

// NewCommandWithClock 创建 Command 并注入时间来源。
func NewCommandWithClock(clk clock.IClock, id, commandType string, aggregateID string, aggregateType string, payload any) *Command {
	message := messaging.NewMessageWithClock(clk, id, messaging.KindCommand, commandType, payload)
	return &Command{
		Message:       *message,
		AggregateID:   aggregateID,
		AggregateType: aggregateType,
	}
}

// GetAggregateID 获取目标聚合 ID。
func (c *Command) GetAggregateID() string {
	return c.AggregateID
}

// GetAggregateType 获取目标聚合类型。
func (c *Command) GetAggregateType() string {
	return c.AggregateType
}

// GetCommandType 获取命令类型（便利方法）。
func (c *Command) GetCommandType() string {
	return c.GetType()
}

// WithMetadata 添加元数据（链式调用）。
func (c *Command) WithMetadata(key, value string) *Command {
	c.SetMetadata(key, value)
	return c
}

// CloneMessageEnvelope 返回命令信封副本，供异步传输在入队前冻结可变字段。
func (c *Command) CloneMessageEnvelope() messaging.IMessage {
	if c == nil {
		return nil
	}
	clone := &Command{
		Message:       messaging.CloneMessage(c.Message),
		AggregateID:   c.AggregateID,
		AggregateType: c.AggregateType,
	}
	return clone
}
