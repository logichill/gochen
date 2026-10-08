package outbox

import (
	"context"
	"time"

	"gochen/eventing"
)

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
