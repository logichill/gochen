// Package audited 提供 “CRUD + 审计/软删/恢复” 的应用服务（用例编排）模板。
package audited

import (
	"context"

	appcrud "gochen/app/crud"
	"gochen/domain"
	"gochen/domain/audited"
	"gochen/domain/crud"
	"gochen/errors"
	"gochen/validate"
)

// IApplication 抽象Application能力接口。
type IApplication[T domain.IEntity[ID], ID comparable] interface {
	appcrud.IApplication[T, ID]

	AuditStore() audited.IAuditStore
	AuditTrail(ctx context.Context, id ID, offset, limit int) ([]audited.AuditRecord, error)
	ListDeleted(ctx context.Context, offset, limit int) ([]T, error)
	Restore(ctx context.Context, id ID, by string) error
	Purge(ctx context.Context, id ID) error
}

// Application 定义Application。
type Application[T domain.IEntity[ID], ID comparable] struct {
	*appcrud.Application[T, ID]

	// repo 通过 Application.Repository() 访问，无需重复持有。
	txRepo      appcrud.ITransactional
	auditStore  audited.IAuditStore
	restoreRepo audited.IRestoreRepository[T, ID]
	deletedRepo audited.IDeletedQueryRepository[T, ID]
}

// isAuditedEntityType 判断AuditedEntity类型。
func isAuditedEntityType[T domain.IEntity[ID], ID comparable]() bool {
	var zero T
	_, ok := any(zero).(audited.IAuditedEntity[ID])
	return ok
}

// NewApplication 创建 audited 应用服务实例。
//
// 约束：
// - entity 类型必须实现 audited.IAuditedEntity；
// - repo 必须同时支持事务（appcrud.ITransactional）与 audited 的 Restore/DeletedList 扩展能力；
// - auditStore 必须非空，用于持久化审计记录。
//
// 参数：
// - repo：实体仓储（承载 audited 的读写与恢复/已删查询能力）。
// - validator：可选校验器（配合 AutoValidate 在写入前执行实体校验）。
// - config：服务配置（nil 表示使用默认配置）。
// - auditStore：审计存储（用于持久化审计记录）。
//
// 返回：
// - err：缺少必需依赖/能力时返回错误。
func NewApplication[T domain.IEntity[ID], ID comparable](
	repo crud.IRepository[T, ID],
	validator validate.IValidator,
	config *appcrud.ServiceConfig,
	auditStore audited.IAuditStore,
) (*Application[T, ID], error) {
	if repo == nil {
		return nil, errors.NewCode(errors.InvalidInput, "repository cannot be nil")
	}
	if !isAuditedEntityType[T]() {
		return nil, errors.NewCode(errors.InvalidInput, "entity type is not audited")
	}
	if auditStore == nil {
		return nil, errors.NewCode(errors.InvalidInput, "auditStore cannot be nil")
	}
	txRepo, ok := repo.(appcrud.ITransactional)
	if !ok {
		return nil, errors.NewCode(errors.InvalidInput, "repository must implement appcrud.ITransactional for audited writes")
	}
	restoreRepo, ok := any(repo).(audited.IRestoreRepository[T, ID])
	if !ok {
		return nil, errors.NewCode(errors.InvalidInput, "repository must implement audited.IRestoreRepository for audited restore")
	}
	deletedRepo, ok := any(repo).(audited.IDeletedQueryRepository[T, ID])
	if !ok {
		return nil, errors.NewCode(errors.InvalidInput, "repository must implement audited.IDeletedQueryRepository for audited deleted list")
	}

	concrete, err := appcrud.NewApplication(repo, validator, config)
	if err != nil {
		return nil, err
	}
	return &Application[T, ID]{
		Application: concrete,
		txRepo:      txRepo,
		auditStore:  auditStore,
		restoreRepo: restoreRepo,
		deletedRepo: deletedRepo,
	}, nil
}

func (s *Application[T, ID]) AuditStore() audited.IAuditStore { return s.auditStore }

func (s *Application[T, ID]) asAuditedEntity(entity T) (audited.IAuditedEntity[ID], error) {
	ae, ok := any(entity).(audited.IAuditedEntity[ID])
	if !ok {
		return nil, errors.NewCode(errors.Internal, "entity does not implement audited.IAuditedEntity")
	}
	return ae, nil
}

func (s *Application[T, ID]) ListDeleted(ctx context.Context, offset, limit int) ([]T, error) {
	return s.deletedRepo.ListDeleted(ctx, offset, limit)
}
