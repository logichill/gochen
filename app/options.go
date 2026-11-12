package app

import (
	"context"

	"gochen/auth"
	"gochen/domain/audited"
	"gochen/domain/crud"
)

// IDescriptorProvider 暴露应用构造期冻结的资源动作清单；返回值应为独立快照。
type IDescriptorProvider interface {
	Descriptor() auth.Descriptor
}

// ITransactionRunner 在事务上下文中执行回调；回调失败时回滚全部业务与审计写入。
// 实现应传播取消，使用传入的 txCtx 参与同源事务，并在提交后执行登记的回调。
// 提交后回调失败必须保留已提交的错误语义，不能伪装为可安全重试的回滚。
// 外部注入的数据源仍由调用方关闭。
type ITransactionRunner interface {
	WithinTx(ctx context.Context, fn func(txCtx context.Context) error) error
}

// IAuditRepository 组合应用审计必需的读写及数据源声明。
// 写入必须参与业务仓储通过 txCtx 传播的同源事务；数据源标识相等不能替代事务参与。
// 历史读取按记录的维度快照过滤，不依赖实体仍然存在。
type IAuditRepository interface {
	audited.IAuditStore
	audited.IAuditQueryStore
	crud.IDataSourceProvider
}

// Projector 在生成审计快照前投影独立实体副本。
type Projector[T any] func(entity T) (any, error)

// Limits 是构造期冻结的应用上限。
type Limits struct {
	MaxBulkSize int
	MaxPageSize int
}

const (
	defaultMaxBulkSize = 1000
	defaultMaxPageSize = 1000
)

// Normalize 补齐非正数上限。
func (l Limits) Normalize() Limits {
	if l.MaxBulkSize <= 0 {
		l.MaxBulkSize = defaultMaxBulkSize
	}
	if l.MaxPageSize <= 0 {
		l.MaxPageSize = defaultMaxPageSize
	}
	return l
}
