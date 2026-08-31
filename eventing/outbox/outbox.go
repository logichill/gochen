// Package outbox 实现 Outbox Pattern，确保事件发布的可靠性。
//
// Outbox Pattern 解决的问题：
// 1. 聚合保存和事件发布的原子性。
// 2. 事件发布失败时的重试机制。
// 3. 分布式系统中的最终一致性保证。
//
// 本包的 DLQ 处理“事件尚未成功发布”且持久重试耗尽的 Outbox 记录，保留查询与重投语义。
// 消息已经投递给 handler 后发生的处理失败属于 messaging/deadletter.ISink 的职责；两者处于
// 发布链路的不同阶段，不共享状态契约。
package outbox

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"gochen/contextx"
	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/registry"
	"gochen/eventing/upcast"
	"gochen/policy/retry"
)

const defaultClaimLease = 5 * time.Minute

// DefaultOutboxTable 是 Outbox 记录的默认表名，作为 sqlstore 各组件（repo/batch/cleanup 等）的统一默认来源。
const DefaultOutboxTable = "event_outbox"

// OutboxStatus 定义发件箱状态枚举。
type OutboxStatus string

const (
	// OutboxStatusPending 是常量。
	OutboxStatusPending OutboxStatus = "pending" // 待发布
	// OutboxStatusProcessing 表示记录已被某个 publisher claim，正在发布中。
	OutboxStatusProcessing OutboxStatus = "processing"
	// OutboxStatusPublished 是常量。
	OutboxStatusPublished OutboxStatus = "published" // 已发布
	// OutboxStatusFailed 是常量。
	OutboxStatusFailed OutboxStatus = "failed" // 发布失败
	// OutboxStatusDeadLettered 表示记录已迁入 DLQ，原 Outbox 行保留为终态且不再被 claim。
	OutboxStatusDeadLettered OutboxStatus = "dead_lettered"
)

// OutboxEntry 表示一个待发布的事件记录。
type OutboxEntry[ID comparable] struct {
	ID            int64        `json:"id" gorm:"primaryKey;autoIncrement"`
	AggregateID   ID           `json:"aggregate_id" gorm:"index;not null"`
	AggregateType string       `json:"aggregate_type" gorm:"index;not null"`
	EventID       string       `json:"event_id" gorm:"uniqueIndex;not null"`
	EventType     string       `json:"event_type" gorm:"index;not null"`
	EventData     string       `json:"event_data" gorm:"type:text;not null"` // JSON 序列化的事件数据
	Status        OutboxStatus `json:"status" gorm:"index;not null;default:'pending'"`
	ClaimToken    string       `json:"claim_token,omitempty" gorm:"index;not null;default:''"`
	CreatedAt     time.Time    `json:"created_at" gorm:"not null"`
	PublishedAt   *time.Time   `json:"published_at,omitempty"`
	RetryCount    int          `json:"retry_count" gorm:"default:0"`
	LastError     string       `json:"last_error,omitempty" gorm:"type:text"`
	LeaseUntil    *time.Time   `json:"lease_until,omitempty" gorm:"index"`
	NextRetryAt   *time.Time   `json:"next_retry_at,omitempty" gorm:"index"`
}

func (OutboxEntry[ID]) TableName() string {
	return DefaultOutboxTable
}

// IOutboxRepository 抽象发件箱仓储能力接口。
type IOutboxRepository[ID comparable] interface {
	// SaveWithEvents 在同一事务中保存聚合事件和 Outbox 记录
	SaveWithEvents(ctx context.Context, aggregateID ID, events []eventing.Event[ID]) error

	// ClaimPendingEntries 原子 claim 一批待发布的 Outbox 记录
	ClaimPendingEntries(ctx context.Context, limit int) ([]OutboxEntry[ID], error)

	// MarkAsPublished 仅在 claim token 匹配时标记记录为已发布
	MarkAsPublished(ctx context.Context, entryID int64, claimToken string) error

	// MarkAsFailed 仅在 claim token 匹配时标记记录为发布失败，并设置下次重试时间
	MarkAsFailed(ctx context.Context, entryID int64, claimToken string, errorMsg string, nextRetryAt time.Time) error

	// RenewClaim 延长已 claim 记录的 lease，避免长时间发布过程中被其他 worker 重新 claim
	RenewClaim(ctx context.Context, entryID int64, claimToken string) error

	// DeletePublished 删除已发布的记录（清理历史数据）
	DeletePublished(ctx context.Context, olderThan time.Time) error
}

// IOutboxPublisher 抽象发件箱Publisher能力接口。
type IOutboxPublisher interface {
	// Start 启动后台发布任务
	Start(ctx context.Context) error

	// Stop 停止后台发布任务（ctx 用于控制等待上界）。
	Stop(ctx context.Context) error

	// PublishPending 手动触发发布待处理的事件
	PublishPending(ctx context.Context) error
}

// OutboxConfig 定义发件箱配置。
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

	// ClaimLease 是单条记录被 publisher claim 后的租约时长。
	ClaimLease time.Duration `json:"claim_lease"`

	// ClaimRenewInterval 是发布过程中续约 claim 的间隔；为 0 时默认使用 ClaimLease 的一半。
	ClaimRenewInterval time.Duration `json:"claim_renew_interval"`

	// MarkPublishedAttempts 是事件已成功发布后写回 published 状态的总尝试次数。
	MarkPublishedAttempts int `json:"mark_published_attempts"`
}

func DefaultOutboxConfig() OutboxConfig {
	return OutboxConfig{
		PublishInterval:       5 * time.Second,
		BatchSize:             100,
		MaxRetries:            5,
		RetryInterval:         30 * time.Second,
		CleanupInterval:       1 * time.Hour,
		RetentionPeriod:       7 * 24 * time.Hour, // 保留 7 天
		ClaimLease:            defaultClaimLease,
		MarkPublishedAttempts: 3,
	}
}

// NormalizeOutboxConfig 为零值字段填充默认值，并约束 ClaimRenewInterval 必须小于 ClaimLease。
// 导出以供 sqlstore 等子包复用，避免归一化逻辑重复。
func NormalizeOutboxConfig(cfg OutboxConfig) OutboxConfig {
	defaults := DefaultOutboxConfig()
	if cfg.PublishInterval <= 0 {
		cfg.PublishInterval = defaults.PublishInterval
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = defaults.BatchSize
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = defaults.MaxRetries
	}
	if cfg.RetryInterval <= 0 {
		cfg.RetryInterval = defaults.RetryInterval
	}
	if cfg.CleanupInterval <= 0 {
		cfg.CleanupInterval = defaults.CleanupInterval
	}
	if cfg.RetentionPeriod <= 0 {
		cfg.RetentionPeriod = defaults.RetentionPeriod
	}
	if cfg.ClaimLease <= 0 {
		cfg.ClaimLease = defaults.ClaimLease
	}
	if cfg.ClaimRenewInterval <= 0 || cfg.ClaimRenewInterval >= cfg.ClaimLease {
		cfg.ClaimRenewInterval = cfg.ClaimLease / 2
	}
	if cfg.ClaimRenewInterval <= 0 {
		cfg.ClaimRenewInterval = time.Second
	}
	if cfg.MarkPublishedAttempts <= 0 {
		cfg.MarkPublishedAttempts = defaults.MarkPublishedAttempts
	}
	return cfg
}

type claimLeaseProvider interface {
	GetClaimLease() time.Duration
}

func normalizeOutboxConfigForRepository[ID comparable](cfg OutboxConfig, repo IOutboxRepository[ID]) (OutboxConfig, error) {
	requestedLease := cfg.ClaimLease
	requestedRenewInterval := cfg.ClaimRenewInterval
	cfg = NormalizeOutboxConfig(cfg)

	provider, ok := any(repo).(claimLeaseProvider)
	if !ok {
		return cfg, nil
	}
	repoLease := provider.GetClaimLease()
	if repoLease <= 0 {
		return cfg, nil
	}
	if requestedLease > 0 && requestedLease != repoLease {
		return OutboxConfig{}, errors.NewCode(errors.InvalidInput, "outbox claim lease mismatch between publisher and repository").
			WithContext("publisher_claim_lease", requestedLease.String()).
			WithContext("repository_claim_lease", repoLease.String())
	}

	cfg.ClaimLease = repoLease
	if requestedRenewInterval <= 0 || requestedRenewInterval >= cfg.ClaimLease {
		cfg.ClaimRenewInterval = cfg.ClaimLease / 2
		if cfg.ClaimRenewInterval <= 0 {
			cfg.ClaimRenewInterval = time.Second
		}
	}
	return cfg, nil
}

func EventToOutboxEntry[ID comparable](aggregateID ID, event eventing.Event[ID]) (*OutboxEntry[ID], error) {
	eventData, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}

	return &OutboxEntry[ID]{
		AggregateID: aggregateID,
		// 以下字段在 SQL Outbox 表结构中是核心索引/约束列：
		// - AggregateType：用于聚合维度过滤与诊断
		// - EventID：用于去重（unique）
		AggregateType: event.GetAggregateType(),
		EventID:       event.GetID(),
		EventType:     event.GetType(),
		EventData:     string(eventData),
		Status:        OutboxStatusPending,
		CreatedAt:     time.Now(),
	}, nil
}

// ToEventWith 转换事件。
func (entry *OutboxEntry[ID]) ToEventWith(
	reg *registry.Registry,
	upgraders *upcast.UpgraderRegistry,
) (eventing.Event[ID], error) {
	return entry.toEventWithContext(contextx.Background(), reg, upgraders)
}

func (entry *OutboxEntry[ID]) toEventWithContext(
	ctx context.Context,
	reg *registry.Registry,
	upgraders *upcast.UpgraderRegistry,
) (eventing.Event[ID], error) {
	var event eventing.Event[ID]
	decoder := json.NewDecoder(strings.NewReader(entry.EventData))
	decoder.UseNumber() // 关键：数字保持为 json.Number 而非 float64
	if err := decoder.Decode(&event); err != nil {
		return eventing.Event[ID]{}, err
	}

	if reg == nil {
		return eventing.Event[ID]{}, errors.NewCode(errors.InvalidInput, "event registry cannot be nil")
	}
	if upgraders == nil {
		return eventing.Event[ID]{}, errors.NewCode(errors.InvalidInput, "event upgrader registry cannot be nil")
	}

	if !reg.HasEvent(event.GetType()) {
		return eventing.Event[ID]{}, errors.NewCode(errors.NotFound, "unknown event type").
			WithContext("event_type", event.GetType()).
			WithContext("event_id", event.GetID())
	}

	// 对已注册的事件类型执行数据升级与强类型反序列化，
	// 确保下游 handler 可以按领域事件类型进行断言。
	upgraded, err := upcast.UpgradeEventPayload(ctx, reg, upgraders, &event)
	if err != nil {
		return eventing.Event[ID]{}, err
	}
	return *upgraded, nil
}

// ShouldRetry 判断是否应该重试。
func (entry *OutboxEntry[ID]) ShouldRetry(maxRetries int) bool {
	return entry.Status == OutboxStatusFailed &&
		entry.RetryCount < maxRetries &&
		(entry.NextRetryAt == nil || time.Now().After(*entry.NextRetryAt))
}

func (entry *OutboxEntry[ID]) canProceedWithClaim() bool {
	if entry == nil {
		return false
	}
	if entry.Status != OutboxStatusProcessing {
		return true
	}
	if entry.ClaimToken == "" || entry.LeaseUntil == nil {
		return false
	}
	return time.Now().Before(*entry.LeaseUntil)
}

// CalculateNextRetryTime 计算下次重试时间（指数退避）。
func (entry *OutboxEntry[ID]) CalculateNextRetryTime(baseInterval time.Duration) time.Time {
	retryCount := entry.RetryCount
	if retryCount < 0 {
		retryCount = 0
	}
	delay := retry.ComputeDelay(retry.Config{
		InitialDelay:  baseInterval,
		BackoffFactor: 2,
		MaxDelay:      durationMulCap(baseInterval, 32),
		JitterRatio:   0,
	}, retryCount+1)
	return time.Now().Add(delay)
}

func durationMulCap(base time.Duration, factor int64) time.Duration {
	if base <= 0 || factor <= 0 {
		return 0
	}
	const maxDuration = time.Duration(1<<63 - 1)
	if base > maxDuration/time.Duration(factor) {
		return maxDuration
	}
	return base * time.Duration(factor)
}
