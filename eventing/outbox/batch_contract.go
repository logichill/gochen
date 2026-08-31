package outbox

import (
	"context"
	"time"
)

// IBatchRepository 批量操作接口。
//
// 提供批量操作方法，减少数据库 IO，提升性能。
type IBatchRepository interface {
	// MarkAsPublishedBatch 批量标记为已发布
	MarkAsPublishedBatch(ctx context.Context, entries []ClaimedEntry) error

	// MarkAsFailedBatch 批量标记为失败
	MarkAsFailedBatch(ctx context.Context, entries []FailedEntry) error

	// DeletePublishedBatch 批量删除已发布记录
	DeletePublishedBatch(ctx context.Context, entryIDs []int64) error
}

// FailedEntry 失败记录信息。
type FailedEntry struct {
	ClaimedEntry
	Error       string
	NextRetryAt time.Time
}

// ClaimedEntry 表示一条已被 claim 的 Outbox 记录。
type ClaimedEntry struct {
	ID         int64
	ClaimToken string
}

// BatchPublisher 支持批量发布的发布器。
//
// 对 ParallelPublisher 的封装，提供批量标记功能。
type BatchPublisher[ID comparable] struct {
	*ParallelPublisher[ID]
	batchOps IBatchRepository
}

// NewBatchPublisher 创建批量Publisher。
func NewBatchPublisher[ID comparable](publisher *ParallelPublisher[ID], batchOps IBatchRepository) *BatchPublisher[ID] {
	return &BatchPublisher[ID]{
		ParallelPublisher: publisher,
		batchOps:          batchOps,
	}
}
