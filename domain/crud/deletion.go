package crud

import (
	"context"
	"gochen/domain"
	"gochen/domain/spec"
)

// IDeletedReadRepository 表示受规约约束的包含已删除记录读取能力。
// 永久清除不要求仓储同时提供恢复能力。
type IDeletedReadRepository[T domain.IEntity[ID], ID comparable] interface {
	// GetWithDeleted 读取实体（包含已删除记录）。
	GetWithDeleted(ctx context.Context, id ID, s spec.Spec) (T, error)
}

// IRestoreRepository 表示恢复已软删实体的条件写入能力。
//
// 说明：
// - `Get(ctx, id, s)` 只返回未删除实体（deleted_at IS NULL）；
// - Restore 需要拿到“包含已删除”的实体以驱动状态机（Restore）并更新落库；
// - 读取由独立 IDeletedReadRepository 提供，两种能力均受规约约束。
type IRestoreRepository[T domain.IEntity[ID], ID comparable] interface {
	// Restore 恢复实体；要求当前行已软删，作为原子条件。
	Restore(ctx context.Context, e T, s spec.Spec) error
}

// IDeletedQueryRepository 表示支持“查询已删除列表”的仓储能力。
//
// 说明：
// - 该能力用于 CRUD 的 ListDeleted 等用例；
// - 默认 ORM Repo 会对实现 `domain.ISoftDeletable` 的实体提供该能力。
type IDeletedQueryRepository[T domain.IEntity[ID], ID comparable] interface {
	// ListDeleted 返回已软删的实体列表（仅删除态），按规约过滤。
	ListDeleted(ctx context.Context, offset, limit int, s spec.Spec) ([]T, error)
}
