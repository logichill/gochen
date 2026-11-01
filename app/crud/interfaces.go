package crud

import (
	"context"
	"gochen/domain/spec"

	"gochen/app/internal/execution"
	"gochen/app/query"
	"gochen/auth"
	"gochen/domain"
	domaudited "gochen/domain/audited"
	domcrud "gochen/domain/crud"
)

// IReadRepository 是只读应用要求的读端口：按 ID 读取 + 统一查询端口。
//
// 列表与计数统一走查询端口，Exists 由 Get 派生。
type IReadRepository[T domain.IEntity[ID], ID comparable] interface {
	Get(ctx context.Context, id ID, s spec.Spec) (T, error)
	query.IQueryableRepository[T]
}

// Repository 是 crud 应用要求的仓储组合：读写端口、查询端口与事务端口。
type Repository[T domain.IEntity[ID], ID comparable] interface {
	domcrud.IRepository[T, ID]
	IReadRepository[T, ID]
	execution.TxRunner
}

// IDescriptorProvider 暴露应用资源的动作清单与基础配额。
type IDescriptorProvider interface {
	Descriptor() auth.Descriptor
	MaxPageSize() int
	MaxBulkSize() int
}

// IReadApplication 声明只读用例端口。
type IReadApplication[T domain.IEntity[ID], ID comparable] interface {
	Get(ctx context.Context, id ID) (T, error)
	List(ctx context.Context, offset, limit int) ([]T, error)
	Query(ctx context.Context, opts query.QueryOptions) ([]T, error)
	QueryCount(ctx context.Context, opts query.QueryOptions) (int64, error)
	Count(ctx context.Context) (int64, error)
	AuditTrail(ctx context.Context, id ID, offset, limit int) ([]domaudited.AuditRecord, error)
}

// IWriteApplication 声明标准写入用例端口。
type IWriteApplication[T domain.IEntity[ID], ID comparable] interface {
	Create(ctx context.Context, entity T) error
	Update(ctx context.Context, entity T) error
	Delete(ctx context.Context, id ID) error
	DeleteBulk(ctx context.Context, ids []ID) error
}

// IActionExecutor 声明自定义业务动作的执行端口。
type IActionExecutor[T domain.IEntity[ID], ID comparable] interface {
	Execute(ctx context.Context, action string, id ID, fn func(ctx context.Context, e T) error, opts ...ExecOption) (T, error)
	ExecuteNew(ctx context.Context, action string, fn func(ctx context.Context) (T, error), opts ...ExecOption) (T, error)
	ExecuteBulk(ctx context.Context, action string, ids []ID, fn func(ctx context.Context, e T) error, opts ...ExecOption) ([]T, error)
	ExecuteNewBulk(ctx context.Context, action string, fn func(ctx context.Context) ([]T, error), opts ...ExecOption) ([]T, error)
}

// IApplication 组合读写、执行与元数据，是通用 CRUD 应用的完整契约。
//
// 传输层（如 rest）依赖这个接口而不是自己声明一份副本：接口归 app 层所有，
// 新增用例端口时不必逐个同步框架内的副本。
type IApplication[T domain.IEntity[ID], ID comparable] interface {
	IDescriptorProvider
	IReadApplication[T, ID]
	IWriteApplication[T, ID]
	IActionExecutor[T, ID]
}

var _ IApplication[domain.IEntity[int64], int64] = (*Application[domain.IEntity[int64], int64])(nil)
