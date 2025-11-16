package command

import (
	"time"

	"gochen/messaging"
)

// Command 命令实现
//
// Command 是 Message 的特化，用于表示系统中的写操作意图。
// 遵循 CQRS 模式，命令不返回结果（或仅返回成功/失败状态）。
//
// 设计原则：
//   - 命令是不可变的
//   - 命令应该是幂等的（基于 ID）
//   - 命令包含执行所需的所有信息
//   - 命令针对特定聚合根（通过 AggregateID 标识）
type Command struct {
	messaging.Message // 嵌入 Message，继承所有 IMessage 能力

	// AggregateID 目标聚合根 ID
	// 用于命令路由和并发控制
	AggregateID int64 `json:"aggregate_id"`

	// AggregateType 目标聚合类型
	// 例如："User", "Order", "Product"
	AggregateType string `json:"aggregate_type"`
}

// NewCommand 创建命令
//
// 参数：
//   - id: 命令唯一标识（建议使用 UUID）
//   - commandType: 命令类型（例如："CreateUser", "UpdateOrder"）
//   - aggregateID: 目标聚合 ID
//   - aggregateType: 目标聚合类型
//   - payload: 命令数据
//
// 返回：
//   - *Command: 初始化的命令实例
func NewCommand(id, commandType string, aggregateID int64, aggregateType string, payload interface{}) *Command {
	cmd := &Command{
		Message: messaging.Message{
			ID:        id,
			Type:      messaging.MessageTypeCommand, // 使用预定义的命令类型
			Timestamp: time.Now(),
			Payload:   payload,
			Metadata:  make(map[string]interface{}),
		},
		AggregateID:   aggregateID,
		AggregateType: aggregateType,
	}

	// 将聚合信息存入元数据，便于中间件访问
	cmd.SetMetadata("aggregate_id", aggregateID)
	cmd.SetMetadata("aggregate_type", aggregateType)
	cmd.SetMetadata("command_type", commandType)

	return cmd
}

// GetAggregateID 获取目标聚合 ID
func (c *Command) GetAggregateID() int64 {
	return c.AggregateID
}

// GetAggregateType 获取目标聚合类型
func (c *Command) GetAggregateType() string {
	return c.AggregateType
}

// GetCommandType 获取命令类型（便利方法）
func (c *Command) GetCommandType() string {
	if cmdType, ok := c.GetMetadata()["command_type"].(string); ok {
		return cmdType
	}
	return c.Type // 回退到消息类型
}

// WithMetadata 添加元数据（链式调用）
func (c *Command) WithMetadata(key string, value interface{}) *Command {
	c.SetMetadata(key, value)
	return c
}

// WithUserID 设置用户 ID（便利方法）
func (c *Command) WithUserID(userID int64) *Command {
	c.UserID = userID
	c.SetMetadata("user_id", userID)
	return c
}

// WithRequestID 设置请求 ID（便利方法）
func (c *Command) WithRequestID(requestID string) *Command {
	c.RequestID = requestID
	c.SetMetadata("request_id", requestID)
	return c
}

// WithCorrelationID 设置关联 ID（用于追踪）
func (c *Command) WithCorrelationID(correlationID string) *Command {
	c.SetMetadata("correlation_id", correlationID)
	return c
}

// WithCausationID 设置因果 ID（触发此命令的事件 ID）
func (c *Command) WithCausationID(causationID string) *Command {
	c.SetMetadata("causation_id", causationID)
	return c
}
