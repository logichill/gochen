// Package audited 提供 “CRUD + 审计/软删/恢复” 的应用服务（用例编排）模板。
package audited

import (
	"sync"

	appcrud "gochen/app/crud"
	"gochen/domain"
	domaudited "gochen/domain/audited"
	"gochen/domain/crud"
	"gochen/errors"
)

// Application 编排带审计记录、软删除与恢复的 CRUD 用例。
type Application[T domain.IEntity[ID], ID comparable] struct {
	*appcrud.Application[T, ID]

	// repo 通过 Application.Repository() 访问，无需重复持有。
	txRepo         appcrud.ITransactional
	auditStore     domaudited.IAuditStore
	restoreRepo    domaudited.IRestoreRepository[T, ID]
	deletedRepo    domaudited.IDeletedQueryRepository[T, ID]
	resourceKind   string
	projectorMu    sync.RWMutex
	auditProjector AuditProjector[T]
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
// - auditStore：审计存储（用于持久化审计记录）。
// - options：构造期应用配置、校验器和生命周期钩子。
//
// 返回：
// - err：缺少必需依赖/能力时返回错误。
func NewApplication[T domain.IEntity[ID], ID comparable](
	repo crud.IRepository[T, ID],
	auditStore domaudited.IAuditStore,
	options *appcrud.Options[T, ID],
) (*Application[T, ID], error) {
	if repo == nil {
		return nil, errors.NewCode(errors.InvalidInput, "repository cannot be nil")
	}
	if !domaudited.IsEntityType[T]() {
		return nil, errors.NewCode(errors.InvalidInput, "entity type is not audited")
	}
	if auditStore == nil {
		return nil, errors.NewCode(errors.InvalidInput, "auditStore cannot be nil")
	}
	txRepo, ok := repo.(appcrud.ITransactional)
	if !ok {
		return nil, errors.NewCode(errors.InvalidInput, "repository must implement appcrud.ITransactional for audited writes")
	}
	restoreRepo, ok := any(repo).(domaudited.IRestoreRepository[T, ID])
	if !ok {
		return nil, errors.NewCode(errors.InvalidInput, "repository must implement audited.IRestoreRepository for audited restore")
	}
	deletedRepo, ok := any(repo).(domaudited.IDeletedQueryRepository[T, ID])
	if !ok {
		return nil, errors.NewCode(errors.InvalidInput, "repository must implement audited.IDeletedQueryRepository for audited deleted list")
	}

	concrete, err := appcrud.NewApplication(repo, options)
	if err != nil {
		return nil, err
	}
	app := &Application[T, ID]{
		Application: concrete,
		txRepo:      txRepo,
		auditStore:  auditStore,
		restoreRepo: restoreRepo,
		deletedRepo: deletedRepo,
	}
	if kindProvider, ok := any(repo).(interface{ ResourceKind() string }); ok && kindProvider != nil {
		app.resourceKind = kindProvider.ResourceKind()
	}
	return app, nil
}
