package repository

import "context"

// ITransactional 定义支持事务的仓储接口（可选扩展）
//
// 提供事务管理能力，用于保证跨多个操作的数据一致性。
// 适用于需要原子性操作的复杂业务场景。
//
// 使用模式：
//
//	ctx, err := repo.BeginTx(ctx)
//	if err != nil { return err }
//	defer repo.Rollback(ctx)  // 确保异常时回滚
//
//	// 执行多个操作...
//
//	return repo.Commit(ctx)   // 成功时提交
//
// 最佳实践：
//   - 总是使用 defer Rollback 确保异常安全
//   - 避免长时间持有事务，影响数据库性能
//   - 考虑事务隔离级别对并发的影响
type ITransactional interface {
	// BeginTx 开始一个新事务
	//
	// 返回包含事务信息的新 context，后续操作应使用该 context。
	//
	// 参数：
	//   - ctx: 上下文
	//
	// 返回：
	//   - context.Context: 包含事务信息的新上下文
	//   - error: 开始事务失败时返回错误
	BeginTx(ctx context.Context) (context.Context, error)

	// Commit 提交当前事务
	//
	// 使事务中的所有修改永久生效。
	//
	// 参数：
	//   - ctx: 包含事务信息的上下文（由 BeginTx 返回）
	//
	// 返回：
	//   - error: 提交失败时返回错误
	Commit(ctx context.Context) error

	// Rollback 回滚当前事务
	//
	// 撤销事务中的所有修改。通常在 defer 中调用以确保异常安全。
	//
	// 参数：
	//   - ctx: 包含事务信息的上下文（由 BeginTx 返回）
	//
	// 返回：
	//   - error: 回滚失败时返回错误（通常可忽略）
	Rollback(ctx context.Context) error
}
