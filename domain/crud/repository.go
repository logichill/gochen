// Package crud 提供面向简单 CRUD 场景的仓储抽象。
// 该包承载通用 CRUD 仓储接口与查询/批量扩展，
// audited 场景可在此基础上扩展审计与软删能力；eventsourced 与 CRUD 并行建模。
package crud

import (
	"context"
	"gochen/domain/spec"

	"gochen/domain"
)

// IRepository 简单 CRUD 仓储接口。
// 适用于配置记录型数据（字典表、分类等）。
//
// 所有读写都强制携带规约：读取按规约过滤，写入在落库前以规约做 WITH CHECK。
//
// # 错误契约。
//
// 所有方法统一使用 gochen/errors 错误码，调用方可通过 errors.Is(err, code) 判断：
//
//   - Get: 未找到（含被规约过滤）返回 errors.NotFound（返回零值 + NotFound）
//   - Create: 实体不满足规约返回 errors.Forbidden；已存在/唯一约束冲突返回 errors.Conflict
//   - Update: 实体不满足规约返回 errors.Forbidden；未找到返回 errors.NotFound；
//     乐观锁/并发写冲突返回 errors.Concurrency；访问控制列不进入 SET
//   - Delete: 一律物理删除；实体不满足规约返回 errors.Forbidden；
//     未找到返回 errors.NotFound；并发写冲突返回 errors.Concurrency
type IRepository[T domain.IEntity[ID], ID comparable] interface {
	// Get 通过 ID 获取实体；已软删的记录对普通读取不可见。
	Get(ctx context.Context, id ID, s spec.Spec) (T, error)

	// Create 创建实体；写入前以规约做 WITH CHECK。
	Create(ctx context.Context, e T, s spec.Spec) error

	// Update 更新实体；写入前以规约做 WITH CHECK，WHERE 为 id ∧ 规约 ∧ version。
	Update(ctx context.Context, e T, s spec.Spec) error

	// Delete 物理删除实体；传入已加载的实体以带上版本与边界。
	Delete(ctx context.Context, e T, s spec.Spec) error
}

// IPurgeRepository 定义“物理删除（不可恢复）”的可选扩展能力。
//
// 说明：
// - 普通 Delete 已是物理删除，Purge 保留给审计资源的永久删除流程；
// - 读取边界字段取自删除前的实体。
type IPurgeRepository[T domain.IEntity[ID], ID comparable] interface {
	// Purge 永久删除实体（物理删除，不可恢复），包括已软删的行。
	Purge(ctx context.Context, e T, s spec.Spec) error
}

// IBulkOperations 定义批量操作接口（可选扩展）。
//
// Bulk 在本包内是“一次调用处理多个实体”的原子语义：逐条执行与批量执行必须产生
// 相同的 WITH CHECK、错误分类与回滚结果，任一实体失败则全部回滚，不存在部分成功。
// 批量操作的规约对每个实体分别求值。
type IBulkOperations[T domain.IEntity[ID], ID comparable] interface {
	// CreateBulk 批量创建实体。
	CreateBulk(ctx context.Context, entities []T, s spec.Spec) error

	// UpdateBulk 批量更新实体。
	UpdateBulk(ctx context.Context, entities []T, s spec.Spec) error

	// DeleteBulk 批量物理删除实体；传入已加载的实体以带上版本。
	DeleteBulk(ctx context.Context, entities []T, s spec.Spec) error
}
