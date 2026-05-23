package rest

import (
	"context"
	"reflect"

	appcrud "gochen/app/crud"
	"gochen/db/query"
	"gochen/domain"
	"gochen/domain/audited"
)

// IListService 表示列表查询路由需要的最小能力。
type IListService[T domain.IEntity[ID], ID comparable] interface {
	ListByQuery(ctx context.Context, query *query.QueryRequest) ([]T, error)
}

// IPagedListService 表示分页列表路由需要的最小能力。
type IPagedListService[T domain.IEntity[ID], ID comparable] interface {
	ListPage(ctx context.Context, request *query.PageRequest) (*query.PagedResult[T], error)
}

// IGetService 表示详情路由需要的最小能力。
type IGetService[T domain.IEntity[ID], ID comparable] interface {
	Get(ctx context.Context, id ID) (T, error)
}

// ICreateService 表示创建路由需要的最小能力。
type ICreateService[T domain.IEntity[ID], ID comparable] interface {
	Create(ctx context.Context, entity T) error
}

// IUpdateService 表示更新路由需要的最小能力。
type IUpdateService[T domain.IEntity[ID], ID comparable] interface {
	Update(ctx context.Context, entity T) error
}

// IDeleteService 表示删除路由需要的最小能力。
type IDeleteService[ID comparable] interface {
	Delete(ctx context.Context, id ID) error
}

// IAuditedService 是 API 层用于"能力探测"的最小 audited 能力面。
//
// 说明：
//   - audited 扩展是一个聚合能力，实体为 audited 时必须整体满足；
//   - RouteBuilder 只依赖该最小方法集，不绑定具体 app/audited 实现。
type IAuditedService[T any, ID comparable] interface {
	Delete(ctx context.Context, id ID) error
	Purge(ctx context.Context, id ID) error
	Restore(ctx context.Context, id ID, by string) error

	ListDeleted(ctx context.Context, offset, limit int) ([]T, error)
	AuditTrail(ctx context.Context, id ID, offset, limit int) ([]audited.AuditRecord, error)

	AuditStore() audited.IAuditStore
}

func isNilService(svc any) bool {
	if svc == nil {
		return true
	}
	rv := reflect.ValueOf(svc)
	switch rv.Kind() {
	case reflect.Ptr, reflect.Map, reflect.Slice, reflect.Func, reflect.Interface, reflect.Chan:
		return rv.IsNil()
	default:
		return false
	}
}

func (rb *RouteBuilder[T, ID]) listService() (IListService[T, ID], bool) {
	svc, ok := rb.service.(IListService[T, ID])
	return svc, ok
}

func (rb *RouteBuilder[T, ID]) pagedListService() (IPagedListService[T, ID], bool) {
	svc, ok := rb.service.(IPagedListService[T, ID])
	return svc, ok
}

func (rb *RouteBuilder[T, ID]) getService() (IGetService[T, ID], bool) {
	svc, ok := rb.service.(IGetService[T, ID])
	return svc, ok
}

func (rb *RouteBuilder[T, ID]) createService() (ICreateService[T, ID], bool) {
	svc, ok := rb.service.(ICreateService[T, ID])
	return svc, ok
}

func (rb *RouteBuilder[T, ID]) updateService() (IUpdateService[T, ID], bool) {
	svc, ok := rb.service.(IUpdateService[T, ID])
	return svc, ok
}

func (rb *RouteBuilder[T, ID]) deleteService() (IDeleteService[ID], bool) {
	svc, ok := rb.service.(IDeleteService[ID])
	return svc, ok
}

func (rb *RouteBuilder[T, ID]) repositoryProvider() (appcrud.IRepositoryProvider[T, ID], bool) {
	svc, ok := rb.service.(appcrud.IRepositoryProvider[T, ID])
	return svc, ok
}
