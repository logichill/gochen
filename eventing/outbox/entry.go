package outbox

import (
	"time"

	"gochen/policy/retry"
)

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
	ID            int64        `json:"id"`
	AggregateID   ID           `json:"aggregate_id"`
	AggregateType string       `json:"aggregate_type"`
	EventID       string       `json:"event_id"`
	EventType     string       `json:"event_type"`
	EventData     string       `json:"event_data"` // JSON 序列化的事件数据
	Status        OutboxStatus `json:"status"`
	ClaimToken    string       `json:"claim_token,omitempty"`
	CreatedAt     time.Time    `json:"created_at"`
	PublishedAt   *time.Time   `json:"published_at,omitempty"`
	RetryCount    int          `json:"retry_count"`
	LastError     string       `json:"last_error,omitempty"`
	LeaseUntil    *time.Time   `json:"lease_until,omitempty"`
	NextRetryAt   *time.Time   `json:"next_retry_at,omitempty"`
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
