package crud

import (
	"context"

	"gochen/app/query"
	"gochen/domain"
	"gochen/domain/crud"
)

// IReader 表示 CRUD 场景下的读能力集合。
type IReader[T domain.IEntity[ID], ID comparable] interface {
	Get(ctx context.Context, id ID) (T, error)
	List(ctx context.Context, offset, limit int) ([]T, error)
	Count(ctx context.Context) (int64, error)
	Exists(ctx context.Context, id ID) (bool, error)
	ListByQuery(ctx context.Context, query *query.QueryRequest) ([]T, error)
	ListPage(ctx context.Context, request *query.PaginationOptions) (*query.PagedResult[T], error)
	CountByQuery(ctx context.Context, query *query.QueryRequest) (int64, error)
}

// IWriter 表示 CRUD 场景下的写能力集合。
type IWriter[T domain.IEntity[ID], ID comparable] interface {
	Create(ctx context.Context, e T) error
	Update(ctx context.Context, e T) error
	Delete(ctx context.Context, id ID) error
}

// IBatchWriter 表示批量写能力。
type IBatchWriter[T domain.IEntity[ID], ID comparable] interface {
	CreateAll(ctx context.Context, entities []T) error
	UpdateAll(ctx context.Context, entities []T) error
	DeleteAll(ctx context.Context, ids []ID) error
}

// IRepositoryProvider 仅供组合根装配仓储协作，不属于业务调用接口。
// 安全装饰器不会向外转发此能力；需要 L3 时先装配 scoped，再装配 action。
type IRepositoryProvider[T domain.IEntity[ID], ID comparable] interface {
	Repository() crud.IRepository[T, ID]
}

// IConfigProvider 供组合根读取构造期配置快照，不属于业务调用接口。
type IConfigProvider interface {
	Config() *ServiceConfig
}

// IApplication 只包含 CRUD 用例；配置与基础设施不属于业务调用接口。
type IApplication[T domain.IEntity[ID], ID comparable] interface {
	IReader[T, ID]
	IWriter[T, ID]
}
