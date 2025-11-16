package projection

import (
	"context"
	"errors"
	"time"
)

// Checkpoint 投影检查点
//
// 记录投影处理事件的位置，用于进程重启后从上次位置继续处理。
//
// 特性：
//   - 最小化字段设计
//   - 支持多种位置追踪策略
//   - 幂等性保证
type Checkpoint struct {
	// ProjectionName 投影名称（唯一标识）
	ProjectionName string `json:"projection_name" db:"projection_name"`

	// Position 事件位置（序列号）
	// 用于全局事件流的位置追踪
	Position int64 `json:"position" db:"position"`

	// LastEventID 最后处理的事件ID
	// 用于幂等性检查
	LastEventID string `json:"last_event_id" db:"last_event_id"`

	// LastEventTime 最后事件时间
	// 用于监控和调试
	LastEventTime time.Time `json:"last_event_time" db:"last_event_time"`

	// UpdatedAt 更新时间
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// ICheckpointStore 检查点存储接口
//
// 定义最小化的检查点持久化接口，易于第三方实现。
//
// 实现建议：
//   - 使用数据库事务确保一致性
//   - 支持幂等操作
//   - 考虑并发访问
//
// 可选实现：
//   - SQL 数据库（推荐）
//   - NoSQL 数据库
//   - 文件系统
//   - 内存存储（测试用）
type ICheckpointStore interface {
	// Load 加载检查点
	//
	// 参数：
	//   - ctx: 上下文
	//   - projectionName: 投影名称
	//
	// 返回：
	//   - *Checkpoint: 检查点数据
	//   - error: ErrCheckpointNotFound 表示检查点不存在，其他错误表示存储失败
	Load(ctx context.Context, projectionName string) (*Checkpoint, error)

	// Save 保存检查点
	//
	// 参数：
	//   - ctx: 上下文
	//   - checkpoint: 检查点数据
	//
	// 返回：
	//   - error: 保存失败错误
	//
	// 注意：
	//   - 应该是幂等操作（重复保存相同数据不会出错）
	//   - 建议使用 UPSERT 或 INSERT ... ON DUPLICATE KEY UPDATE
	Save(ctx context.Context, checkpoint *Checkpoint) error

	// Delete 删除检查点
	//
	// 参数：
	//   - ctx: 上下文
	//   - projectionName: 投影名称
	//
	// 返回：
	//   - error: 删除失败错误（检查点不存在不是错误）
	//
	// 用途：
	//   - 重建投影前清空检查点
	//   - 删除废弃的投影
	Delete(ctx context.Context, projectionName string) error
}

// 检查点相关错误
var (
	// ErrCheckpointNotFound 检查点不存在
	ErrCheckpointNotFound = errors.New("checkpoint not found")

	// ErrInvalidCheckpoint 无效的检查点数据
	ErrInvalidCheckpoint = errors.New("invalid checkpoint")

	// ErrCheckpointStoreFailed 检查点存储失败
	ErrCheckpointStoreFailed = errors.New("checkpoint store failed")
)

// NewCheckpoint 创建新的检查点
//
// 参数：
//   - projectionName: 投影名称
//   - position: 事件位置
//   - lastEventID: 最后事件ID
//   - lastEventTime: 最后事件时间
//
// 返回：
//   - *Checkpoint: 检查点实例
func NewCheckpoint(projectionName string, position int64, lastEventID string, lastEventTime time.Time) *Checkpoint {
	return &Checkpoint{
		ProjectionName: projectionName,
		Position:       position,
		LastEventID:    lastEventID,
		LastEventTime:  lastEventTime,
		UpdatedAt:      time.Now(),
	}
}

// IsValid 验证检查点数据
//
// 返回：
//   - bool: 是否有效
func (c *Checkpoint) IsValid() bool {
	return c.ProjectionName != "" && c.Position >= 0
}

// Clone 克隆检查点
//
// 返回：
//   - *Checkpoint: 克隆的检查点
func (c *Checkpoint) Clone() *Checkpoint {
	return &Checkpoint{
		ProjectionName: c.ProjectionName,
		Position:       c.Position,
		LastEventID:    c.LastEventID,
		LastEventTime:  c.LastEventTime,
		UpdatedAt:      c.UpdatedAt,
	}
}

// Update 更新检查点位置
//
// 参数：
//   - position: 新位置
//   - eventID: 事件ID
//   - eventTime: 事件时间
func (c *Checkpoint) Update(position int64, eventID string, eventTime time.Time) {
	c.Position = position
	c.LastEventID = eventID
	c.LastEventTime = eventTime
	c.UpdatedAt = time.Now()
}
