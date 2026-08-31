// Package audited 提供 “CRUD + 审计/软删/恢复” 的应用服务（用例编排）模板。
package audited

import (
	"context"
	"sync"

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

// AuditProjector transforms an entity before it is serialized into an audit snapshot.
// Use it to redact sensitive fields, mask PII, or project to a subset of fields.
// Return the entity unchanged (or use a nil projector) to keep the full entity.
type AuditProjector[T any] func(entity T) (any, error)

// Application 定义Application。
type Application[T domain.IEntity[ID], ID comparable] struct {
	*appcrud.Application[T, ID]

	// repo 通过 Application.Repository() 访问，无需重复持有。
	txRepo         appcrud.ITransactional
	auditStore     audited.IAuditStore
	restoreRepo    audited.IRestoreRepository[T, ID]
	deletedRepo    audited.IDeletedQueryRepository[T, ID]
	resourceKind   string
	batchWriter    *BatchWriter[T, ID]
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
	if !audited.IsEntityType[T]() {
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
	app.batchWriter = NewBatchWriter(app)
	return app, nil
}

// WithAuditProjector 注入审计投影函数；用于在审计快照中脱敏或投影实体字段。
// 可在运行期间替换，后续写入使用新投影函数。
func (s *Application[T, ID]) WithAuditProjector(projector AuditProjector[T]) *Application[T, ID] {
	if s == nil || projector == nil {
		return s
	}
	s.projectorMu.Lock()
	s.auditProjector = projector
	s.projectorMu.Unlock()
	return s
}

func (s *Application[T, ID]) projectForAudit(entity T) (any, error) {
	if s == nil {
		return entity, nil
	}
	s.projectorMu.RLock()
	projector := s.auditProjector
	s.projectorMu.RUnlock()
	if projector == nil {
		return entity, nil
	}
	return projector(entity)
}

func (s *Application[T, ID]) AuditStore() audited.IAuditStore { return s.auditStore }

func (s *Application[T, ID]) auditContext(ctx context.Context) context.Context {
	if s == nil || s.resourceKind == "" {
		return ctx
	}
	return audited.WithAuditResourceKind(ctx, s.resourceKind)
}

// CreateAll 批量创建 audited 实体。
func (s *Application[T, ID]) CreateAll(ctx context.Context, entities []T) error {
	return s.batchWriter.CreateAll(ctx, entities)
}

// UpdateAll 批量更新 audited 实体。
func (s *Application[T, ID]) UpdateAll(ctx context.Context, entities []T) error {
	return s.batchWriter.UpdateAll(ctx, entities)
}

// DeleteAll 批量软删 audited 实体。
func (s *Application[T, ID]) DeleteAll(ctx context.Context, ids []ID) error {
	return s.batchWriter.DeleteAll(ctx, ids)
}

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

// GetWithDeleted 按主键读取实体，并允许命中软删除记录。
func (s *Application[T, ID]) GetWithDeleted(ctx context.Context, id ID) (T, error) {
	return s.restoreRepo.GetWithDeleted(ctx, id)
}
