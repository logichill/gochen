package crud

import (
	"context"

	"gochen/domain"
)

// Stage 定义单动作的生命周期钩子三段式：Before、After、PostCommit。
//
// 约定：
//   - Before 在写入前执行，失败会阻断后续校验/写入；
//   - After 在核心写入后、事务提交前执行，失败会回滚本次写入；
//   - PostCommit 在事务真正提交后执行，失败不会回滚已提交写入；
//     需要仓储实现 ITransactional；
//   - 未配置的钩子为 no-op；CRUD 扩展通过 Options.Hooks 在构造期显式注入。
type Stage[P any] struct {
	// Before 在写入前执行。
	Before func(ctx context.Context, payload P) error
	// After 在写入后、事务提交前执行。
	After func(ctx context.Context, payload P) error
	// PostCommit 在事务提交后执行。
	PostCommit func(ctx context.Context, payload P) error
}

// IsZero 报告该 Stage 是否未配置任何钩子。
func (s Stage[P]) IsZero() bool {
	return s.Before == nil && s.After == nil && s.PostCommit == nil
}

// Hooks 定义 CRUD 写入生命周期钩子，按动作（Create/Update/Delete）分组。
type Hooks[T domain.IEntity[ID], ID comparable] struct {
	// Create 创建动作的生命周期钩子（单条与批量共用）。
	Create Stage[T]
	// Update 更新动作的生命周期钩子（单条与批量共用）。
	Update Stage[T]
	// Delete 删除动作的生命周期钩子（单条与批量共用）。
	Delete Stage[ID]
	// Purge 物理删除（硬删除）动作的独立生命周期钩子。
	// 与软删除（Delete）语义不同：物理清除冷数据不应重放 Delete 的
	// 业务副作用（事件派发、级联归档等），其 Before 也不应被“活跃态”
	// 校验卡死（已软删记录对常规读不可见）。
	// 未配置时回退到 Delete 钩子（兼容既有行为）。
	Purge Stage[ID]
	// Restore 恢复已软删实体动作的独立生命周期钩子。
	// 与常规更新（Update）语义不同：恢复不产生新的业务变更事件。
	// 未配置时回退到 Update 钩子（兼容既有行为）。
	Restore Stage[T]
}
