package crud

import (
	"context"

	"gochen/domain"
)

// Hooks 定义 CRUD 写入生命周期钩子集合。
//
// 约定：
// - Before* 在写入前执行，失败会阻断后续校验/写入；
// - After* 在 repository 写入后、事务提交前执行，失败会回滚本次写入；
// - PostCommit* 在事务提交后执行，失败不会回滚已提交写入；
// - 未配置的 hook 为 no-op；CRUD 扩展只通过 SetHooks / rest.WithHooks 显式注入。
type Hooks[T domain.IEntity[ID], ID comparable] struct {
	// BeforeCreate 在 Create 写入前执行。
	BeforeCreate func(ctx context.Context, entity T) error
	// AfterCreate 在 Create 写入后、事务提交前执行。
	AfterCreate func(ctx context.Context, entity T) error
	// PostCommitCreate 在 Create 事务提交后执行；需要 repository 实现 ITransactional。
	PostCommitCreate func(ctx context.Context, entity T) error

	// BeforeUpdate 在 Update 写入前执行。
	BeforeUpdate func(ctx context.Context, entity T) error
	// AfterUpdate 在 Update 写入后、事务提交前执行。
	AfterUpdate func(ctx context.Context, entity T) error
	// PostCommitUpdate 在 Update 事务提交后执行；需要 repository 实现 ITransactional。
	PostCommitUpdate func(ctx context.Context, entity T) error

	// BeforeDelete 在 Delete 写入前执行。
	BeforeDelete func(ctx context.Context, id ID) error
	// AfterDelete 在 Delete 写入后、事务提交前执行。
	AfterDelete func(ctx context.Context, id ID) error
	// PostCommitDelete 在 Delete 事务提交后执行；需要 repository 实现 ITransactional。
	PostCommitDelete func(ctx context.Context, id ID) error
}
