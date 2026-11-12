package crud

import (
	"context"
	"gochen/domain"
	"gochen/domain/spec"
)

// IRestoreRepository 表示支持“读取已软删实体并恢复”的仓储能力。
//
// 说明：
// - `Get(ctx, id, s)` 只返回未删除实体（deleted_at IS NULL）；
// - Restore 需要拿到“包含已删除”的实体以驱动状态机（Restore）并更新落库；
// - 两个方法与普通端口一样受规约约束，已软删记录只能经本组端口读写。
type IRestoreRepository[T domain.IEntity[ID], ID comparable] interface {
	// GetWithDeleted 读取实体（包含已删除记录）。
	GetWithDeleted(ctx context.Context, id ID, s spec.Spec) (T, error)

	// Restore 恢复实体；要求当前行已软删，作为原子条件。
	Restore(ctx context.Context, e T, s spec.Spec) error
}

// IDeletedQueryRepository 表示支持“查询已删除列表”的仓储能力。
//
// 说明：
// - 该能力用于 deletion 的 ListDeleted 等用例；
// - 默认 ORM Repo 会对实现 `domain.ISoftDeletable` 的实体提供该能力。
type IDeletedQueryRepository[T domain.IEntity[ID], ID comparable] interface {
	// ListDeleted 返回已软删的实体列表（仅删除态），按规约过滤。
	ListDeleted(ctx context.Context, offset, limit int, s spec.Spec) ([]T, error)
}
