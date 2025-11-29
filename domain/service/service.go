// Package service 定义服务层接口，支撑 CRUD → Audited → Event Sourcing 的渐进式演进
package service

import (
	"context"

	"gochen/domain/entity"
	repo "gochen/domain/repository"
	"gochen/eventing"
)

// IService 面向简单 CRUD 的服务接口
// 依赖 ICRUDRepository，适合配置/主数据等场景
type IService[T entity.IEntity[ID], ID comparable] interface {
	Create(ctx context.Context, e T) error
	GetByID(ctx context.Context, id ID) (T, error)
	Update(ctx context.Context, e T) error
	Delete(ctx context.Context, id ID) error
	List(ctx context.Context, offset, limit int) ([]T, error)
	Count(ctx context.Context) (int64, error)

	Repository() repo.IRepository[T, ID]
}

// IAuditedService 面向带审计/软删除的服务接口
// 在 CRUD 基础上引入审计追踪、软删/恢复等能力
type IAuditedService[T entity.IAuditedEntity[ID], ID comparable] interface {
	IService[T, ID]

	GetAuditTrail(ctx context.Context, id ID) ([]repo.AuditRecord, error)
	ListDeleted(ctx context.Context, offset, limit int) ([]T, error)
	Restore(ctx context.Context, id ID, by string) error
	SoftDelete(ctx context.Context, id ID, by string) error
	PermanentDelete(ctx context.Context, id ID) error

	AuditedRepository() repo.IAuditedRepository[T, ID]
}

// IEventSourcedService 面向事件溯源聚合的服务接口
// 仅保存事件，不直接持久化当前状态；通过事件重放恢复状态
type IEventSourcedService[A entity.IEventSourcedAggregate[ID], ID comparable] interface {
	// Save 追加未提交事件并清空聚合的未提交队列
	Save(ctx context.Context, aggregate A) error

	// GetByID 重放事件获取聚合
	GetByID(ctx context.Context, id ID) (A, error)

	Exists(ctx context.Context, id ID) (bool, error)
	GetEventHistory(ctx context.Context, id ID) ([]eventing.IEvent, error)
	GetEventHistoryAfter(ctx context.Context, id ID, afterVersion uint64) ([]eventing.IEvent, error)
	GetAggregateVersion(ctx context.Context, id ID) (uint64, error)

	EventSourcedRepository() repo.IEventSourcedRepository[A, ID]
}
