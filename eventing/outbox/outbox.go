// Package outbox 实现 Outbox Pattern，确保事件发布的可靠性
//
// Outbox Pattern 解决的问题：
// 1. 聚合保存和事件发布的原子性
// 2. 事件发布失败时的重试机制
// 3. 分布式系统中的最终一致性保证
package outbox

import (
	"context"
	"encoding/json"
	"time"

	"gochen/eventing"
)

// OutboxStatus 表示 Outbox 记录的状态
type OutboxStatus string

const (
	OutboxStatusPending   OutboxStatus = "pending"   // 待发布
	OutboxStatusPublished OutboxStatus = "published" // 已发布
	OutboxStatusFailed    OutboxStatus = "failed"    // 发布失败
)

// OutboxEntry 表示一个待发布的事件记录
type OutboxEntry struct {
	ID            int64        `json:"id" gorm:"primaryKey;autoIncrement"`
	AggregateID   int64        `json:"aggregate_id" gorm:"index;not null"`
	AggregateType string       `json:"aggregate_type" gorm:"index;not null"`
	EventID       string       `json:"event_id" gorm:"uniqueIndex;not null"`
	EventType     string       `json:"event_type" gorm:"index;not null"`
	EventData     string       `json:"event_data" gorm:"type:text;not null"` // JSON 序列化的事件数据
	Status        OutboxStatus `json:"status" gorm:"index;not null;default:'pending'"`
	CreatedAt     time.Time    `json:"created_at" gorm:"not null"`
	PublishedAt   *time.Time   `json:"published_at,omitempty"`
	RetryCount    int          `json:"retry_count" gorm:"default:0"`
	LastError     string       `json:"last_error,omitempty" gorm:"type:text"`
	NextRetryAt   *time.Time   `json:"next_retry_at,omitempty" gorm:"index"`
}

// TableName 返回数据库表名
func (OutboxEntry) TableName() string {
	return "event_outbox"
}

// IOutboxRepository 定义 Outbox 仓储接口
type IOutboxRepository interface {
	// SaveWithEvents 在同一事务中保存聚合事件和 Outbox 记录
	SaveWithEvents(ctx context.Context, aggregateID int64, events []eventing.Event) error

	// GetPendingEntries 获取待发布的 Outbox 记录
	GetPendingEntries(ctx context.Context, limit int) ([]OutboxEntry, error)

	// MarkAsPublished 标记记录为已发布
	MarkAsPublished(ctx context.Context, entryID int64) error

	// MarkAsFailed 标记记录为发布失败，并设置下次重试时间
	MarkAsFailed(ctx context.Context, entryID int64, errorMsg string, nextRetryAt time.Time) error

	// DeletePublished 删除已发布的记录（清理历史数据）
	DeletePublished(ctx context.Context, olderThan time.Time) error
}

// IOutboxPublisher 定义 Outbox 发布器接口
type IOutboxPublisher interface {
	// Start 启动后台发布任务
	Start(ctx context.Context) error

	// Stop 停止后台发布任务
	Stop() error

	// PublishPending 手动触发发布待处理的事件
	PublishPending(ctx context.Context) error
}

// OutboxConfig Outbox 配置
type OutboxConfig struct {
	// 发布间隔
	PublishInterval time.Duration `json:"publish_interval"`

	// 每次处理的最大记录数
	BatchSize int `json:"batch_size"`

	// 最大重试次数
	MaxRetries int `json:"max_retries"`

	// 重试间隔（指数退避）
	RetryInterval time.Duration `json:"retry_interval"`

	// 历史数据清理间隔
	CleanupInterval time.Duration `json:"cleanup_interval"`

	// 保留已发布记录的时间
	RetentionPeriod time.Duration `json:"retention_period"`
}

// DefaultOutboxConfig 返回默认配置
func DefaultOutboxConfig() OutboxConfig {
	return OutboxConfig{
		PublishInterval: 5 * time.Second,
		BatchSize:       100,
		MaxRetries:      5,
		RetryInterval:   30 * time.Second,
		CleanupInterval: 1 * time.Hour,
		RetentionPeriod: 7 * 24 * time.Hour, // 保留 7 天
	}
}

// EventToOutboxEntry 将事件转换为 Outbox 记录
func EventToOutboxEntry(aggregateID int64, event eventing.Event) (*OutboxEntry, error) {
	eventData, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}

	return &OutboxEntry{
		AggregateID: aggregateID,
		EventType:   event.GetType(),
		EventData:   string(eventData),
		Status:      OutboxStatusPending,
		CreatedAt:   time.Now(),
	}, nil
}

// ToEvent 将 Outbox 记录转换回事件
func (entry *OutboxEntry) ToEvent() (eventing.Event, error) {
	var event eventing.Event
	if err := json.Unmarshal([]byte(entry.EventData), &event); err != nil {
		return eventing.Event{}, err
	}
	return event, nil
}

// ShouldRetry 判断是否应该重试
func (entry *OutboxEntry) ShouldRetry(maxRetries int) bool {
	return entry.Status == OutboxStatusFailed &&
		entry.RetryCount < maxRetries &&
		(entry.NextRetryAt == nil || time.Now().After(*entry.NextRetryAt))
}

// CalculateNextRetryTime 计算下次重试时间（指数退避）
func (entry *OutboxEntry) CalculateNextRetryTime(baseInterval time.Duration) time.Time {
	// 指数退避：baseInterval * 2^retryCount，避免移位溢出
	retryCount := entry.RetryCount
	if retryCount < 0 {
		retryCount = 0
	}
	// 上限 5 次指数放大（2^5 = 32），避免 1<<retryCount 溢出导致负数或超大等待时间
	if retryCount > 5 {
		retryCount = 5
	}
	backoffMultiplier := 1 << retryCount // 2^retryCount，范围 [1,32]

	delay := baseInterval * time.Duration(backoffMultiplier)
	return time.Now().Add(delay)
}
