package saga

import "context"

// ISagaStateStore Saga 状态存储接口
//
// 定义最小化的状态持久化接口，易于第三方实现。
//
// 实现建议：
//   - 使用数据库事务确保一致性
//   - 支持幂等操作
//   - 考虑并发访问
//
// 可选实现：
//   - SQL 数据库（推荐）
//   - NoSQL 数据库
//   - 文件系统
//   - 内存存储（测试用）
type ISagaStateStore interface {
	// Load 加载 Saga 状态
	//
	// 参数：
	//   - ctx: 上下文
	//   - sagaID: Saga ID
	//
	// 返回：
	//   - *SagaState: 状态数据
	//   - error: ErrSagaNotFound 表示不存在，其他错误表示存储失败
	Load(ctx context.Context, sagaID string) (*SagaState, error)

	// Save 保存 Saga 状态
	//
	// 参数：
	//   - ctx: 上下文
	//   - state: 状态数据
	//
	// 返回：
	//   - error: 保存失败错误
	//
	// 注意：
	//   - 应该是幂等操作
	//   - 建议使用 UPSERT
	Save(ctx context.Context, state *SagaState) error

	// Update 更新 Saga 状态
	//
	// 参数：
	//   - ctx: 上下文
	//   - state: 状态数据
	//
	// 返回：
	//   - error: 更新失败错误
	Update(ctx context.Context, state *SagaState) error

	// Delete 删除 Saga 状态
	//
	// 参数：
	//   - ctx: 上下文
	//   - sagaID: Saga ID
	//
	// 返回：
	//   - error: 删除失败错误
	Delete(ctx context.Context, sagaID string) error

	// List 列出所有 Saga 状态（可选，用于监控）
	//
	// 参数：
	//   - ctx: 上下文
	//   - status: 状态过滤（可选）
	//
	// 返回：
	//   - []*SagaState: 状态列表
	//   - error: 查询失败错误
	List(ctx context.Context, status SagaStatus) ([]*SagaState, error)
}
