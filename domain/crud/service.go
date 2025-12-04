// Package crud 提供面向简单 CRUD 场景的服务接口与默认实现。
// audited 与 application 层在此基础上扩展审计 / 业务逻辑。
package crud

import (
	"context"
	"gochen/domain"
)

// IService 面向简单 CRUD 的服务接口
// 依赖 IRepository，适合配置/主数据等场景
type IService[T domain.IEntity[ID], ID comparable] interface {
	Create(ctx context.Context, e T) error
	GetByID(ctx context.Context, id ID) (T, error)
	Update(ctx context.Context, e T) error
	Delete(ctx context.Context, id ID) error
	List(ctx context.Context, offset, limit int) ([]T, error)
	Count(ctx context.Context) (int64, error)

	Repository() IRepository[T, ID]
}

// CRUDService 基于 IRepository 的默认实现，减少样板代码
type CRUDService[T domain.IEntity[ID], ID comparable] struct {
	repository IRepository[T, ID]
}

// NewCRUDService 创建基于 CRUD 仓储的通用服务实现
func NewCRUDService[T domain.IEntity[ID], ID comparable](r IRepository[T, ID]) *CRUDService[T, ID] {
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

func (s *CRUDService[T, ID]) Repository() IRepository[T, ID] { return s.repository }
