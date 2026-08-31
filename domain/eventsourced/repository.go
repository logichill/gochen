package eventsourced

import (
	"context"

	"gochen/domain"
)

// IEventSourcedRepository 事件溯源仓储接口。
// 适用于完全审计型数据（金融交易、积分系统等）
//
// # 错误契约。
//
// 所有方法统一使用 gochen/errors 错误码，调用方可通过 errors.Is(err, code) 判断：
//
//   - Save: 并发冲突（expectedVersion 不匹配）返回 errors.Concurrency；其他为基础设施错误。
//   - Get: 未找到返回 errors.NotFound；恢复失败返回基础设施错误。
//   - GetOrCreate: 不返回 NotFound；不存在时返回 factory 创建的新实例（version=0）；错误仅来自 store/反序列化。
//   - Exists: 仅在查询失败时返回错误，聚合不存在返回 (false, nil)
//   - GetAggregateVersion: 聚合不存在返回 (0, nil)；查询失败返回基础设施错误。
type IEventSourcedRepository[T IEventSourcedAggregate[ID], ID comparable] interface {
	// Save 保存聚合（保存事件，不保存状态）。
	Save(ctx context.Context, aggregate T) error

	// Get 通过 ID 获取聚合。
	// 若聚合不存在，返回 errors.NotFound。
	// 具体实现通过重放事件重建聚合状态。
	Get(ctx context.Context, id ID) (T, error)

	// GetOrCreate 获取或创建聚合。
	// 若聚合存在则返回；若不存在则返回 factory 创建的新实例（version=0）。
	// 适用于"幂等创建"场景。
	GetOrCreate(ctx context.Context, id ID) (T, error)

	// Exists 检查聚合是否存在。
	Exists(ctx context.Context, id ID) (bool, error)

	// GetAggregateVersion 获取聚合的当前版本号。
	// 若聚合不存在，应返回 (0, nil)。
	GetAggregateVersion(ctx context.Context, id ID) (uint64, error)
}

// IDomainEventStore 领域层的事件存储抽象。
//
// 注意：该接口以领域事件（IDomainEvent）为中心，不关心具体存储实现与传输信封，
// 由上层通过适配器对接 eventing/store.IEventStore、Outbox、Snapshot 等基础设施。
//
// # 错误契约。
//
//   - AppendEvents: expectedVersion 不匹配返回 errors.Concurrency（与 eventing/store.IEventStore 一致）；
//     Details 应包含 aggregate_id/expected_version/actual_version
//   - RestoreAggregate: 聚合不存在不是错误（Exists=false）；仅当底层读取/解码失败才返回 error（通常为基础设施错误）
//   - Exists: 仅在查询失败时返回错误，聚合不存在返回 (false, nil)
//   - GetAggregateVersion: 聚合不存在返回 (0, nil)；查询失败返回基础设施错误。
//
// 类型参数：
//   - ID: 聚合根 ID 类型，必须是可比较类型（如 int64、string、uuid.UUID 等）
type IDomainEventStore[ID comparable] interface {
	// AppendEvents 追加领域事件到聚合的事件流中。
	AppendEvents(ctx context.Context, aggregateID ID, events []domain.IDomainEvent, expectedVersion uint64) error

	// RestoreAggregate 根据底层事件流（及可选快照）恢复聚合状态。
	//
	// 返回 RestoreResult 包含：
	//   - Version: 聚合当前版本号（最后一个事件的版本）
	//   - EventCount: 本次重放的事件数量
	//   - FromSnapshot: 是否从快照恢复
	//   - Exists: 聚合是否存在（version > 0）
	//
	// 若聚合不存在，返回 Exists=false，aggregate 保持初始状态。
	RestoreAggregate(ctx context.Context, aggregate IEventSourcedAggregate[ID]) (*RestoreResult, error)

	// Exists 检查聚合是否存在。
	Exists(ctx context.Context, aggregateID ID) (bool, error)

	// GetAggregateVersion 获取聚合当前版本。
	// 若聚合不存在，应返回 (0, nil)。
	GetAggregateVersion(ctx context.Context, aggregateID ID) (uint64, error)
}

// RestoreResult 聚合恢复结果。
type RestoreResult struct {
	// Version 聚合当前版本号（最后一个事件的版本）。
	Version uint64

	// EventCount 本次重放的事件数量。
	EventCount int

	// FromSnapshot 是否从快照恢复。
	FromSnapshot bool

	// SnapshotVersion 快照版本号（若 FromSnapshot=true）。
	SnapshotVersion uint64

	// Exists 聚合是否存在（version > 0）。
	Exists bool
}
