package command

import (
	"gochen/clock"
	"gochen/messaging"
)

var defaultCommandClock = clock.NewRealClock()

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
	return NewCommandWithClock(defaultCommandClock, id, commandType, aggregateID, aggregateType, payload)
}

// NewCommandWithClock 创建Command并带时钟。
func NewCommandWithClock(clk clock.IClock, id, commandType string, aggregateID string, aggregateType string, payload any) *Command {
	if clk == nil {
		clk = defaultCommandClock
	}
	return &Command{
		Message: messaging.Message{
			ID:        id,
			Kind:      messaging.KindCommand,
			Type:      commandType,
			Timestamp: clk.Now(),
			Payload:   messaging.NewPayload(payload),
			Metadata:  messaging.NewMetadata(),
		},
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
