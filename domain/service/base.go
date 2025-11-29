package service

import (
	"context"

	"gochen/domain/entity"
	repo "gochen/domain/repository"
)

// CRUDService 基于 ICRUDRepository 的默认实现，减少样板代码
type CRUDService[T entity.IEntity[ID], ID comparable] struct {
	repository repo.IRepository[T, ID]
}

func NewCRUDService[T entity.IEntity[ID], ID comparable](r repo.IRepository[T, ID]) *CRUDService[T, ID] {
	return &CRUDService[T, ID]{repository: r}
}

func (s *CRUDService[T, ID]) Create(ctx context.Context, e T) error {
	return s.repository.Create(ctx, e)
}
func (s *CRUDService[T, ID]) GetByID(ctx context.Context, id ID) (T, error) {
	return s.repository.GetByID(ctx, id)
}
func (s *CRUDService[T, ID]) Update(ctx context.Context, e T) error {
	return s.repository.Update(ctx, e)
}
func (s *CRUDService[T, ID]) Delete(ctx context.Context, id ID) error {
	return s.repository.Delete(ctx, id)
}
func (s *CRUDService[T, ID]) List(ctx context.Context, o, l int) ([]T, error) {
	return s.repository.List(ctx, o, l)
}
func (s *CRUDService[T, ID]) Count(ctx context.Context) (int64, error) {
	return s.repository.Count(ctx)
}
func (s *CRUDService[T, ID]) Exists(ctx context.Context, id ID) (bool, error) {
	return s.repository.Exists(ctx, id)
}
func (s *CRUDService[T, ID]) Repository() repo.IRepository[T, ID] { return s.repository }

// AuditedService 基于 IAuditedRepository 的默认实现
type AuditedService[T entity.IAuditedEntity[ID], ID comparable] struct {
	CRUDService[T, ID]
	audited repo.IAuditedRepository[T, ID]
}

func NewAuditedService[T entity.IAuditedEntity[ID], ID comparable](r repo.IAuditedRepository[T, ID]) *AuditedService[T, ID] {
	return &AuditedService[T, ID]{CRUDService: CRUDService[T, ID]{repository: r}, audited: r}
}

func (s *AuditedService[T, ID]) GetAuditTrail(ctx context.Context, id ID) ([]repo.AuditRecord, error) {
	return s.audited.GetAuditTrail(ctx, id)
}
func (s *AuditedService[T, ID]) ListDeleted(ctx context.Context, o, l int) ([]T, error) {
	return s.audited.ListDeleted(ctx, o, l)
}
func (s *AuditedService[T, ID]) Restore(ctx context.Context, id ID, by string) error {
	return s.audited.Restore(ctx, id, by)
}
func (s *AuditedService[T, ID]) SoftDelete(ctx context.Context, id ID, by string) error {
	return s.audited.SoftDelete(ctx, id, by)
}
func (s *AuditedService[T, ID]) PermanentDelete(ctx context.Context, id ID) error {
	return s.audited.PermanentDelete(ctx, id)
}
func (s *AuditedService[T, ID]) AuditedRepository() repo.IAuditedRepository[T, ID] { return s.audited }
