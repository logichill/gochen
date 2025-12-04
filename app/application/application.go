// Package application 提供应用服务层实现
package application

import (
	"context"

	"gochen/domain"
	"gochen/domain/crud"
	validation "gochen/validation"
)

// IApplication 应用服务接口
// 扩展基础 CRUD 服务，提供业务逻辑封装和生命周期管理
type IApplication[T domain.IEntity[int64]] interface {
	crud.IService[T, int64]

	// 查询扩展
	ListByQuery(ctx context.Context, query *QueryParams) ([]T, error)
	ListPage(ctx context.Context, options *PaginationOptions) (*PagedResult[T], error)
	CountByQuery(ctx context.Context, query *QueryParams) (int64, error)

	// 批量操作
	CreateBatch(ctx context.Context, entities []T) (*BatchOperationResult, error)
	UpdateBatch(ctx context.Context, entities []T) (*BatchOperationResult, error)
	DeleteBatch(ctx context.Context, ids []int64) (*BatchOperationResult, error)

	// 业务操作钩子
	Validate(entity T) error
	BeforeCreate(ctx context.Context, entity T) error
	AfterCreate(ctx context.Context, entity T) error
	BeforeUpdate(ctx context.Context, entity T) error
	AfterUpdate(ctx context.Context, entity T) error
	BeforeDelete(ctx context.Context, id int64) error
	AfterDelete(ctx context.Context, id int64) error

	// 服务配置
	GetConfig() *ServiceConfig
}

// ServiceConfig 服务配置
type ServiceConfig struct {
	// 自动验证
	AutoValidate bool

	// 自动时间戳
	AutoTimestamp bool

	// 软删除
	SoftDelete bool

	// 审计字段
	AuditFields bool

	// 最大批量操作数量
	MaxBatchSize int

	// 最大单页大小（分页查询）
	MaxPageSize int

	// 启用缓存
	EnableCache bool

	// 缓存过期时间（秒）
	CacheTTL int

	// 启用审计日志
	EnableAudit bool

	// 乐观锁
	OptimisticLock bool

	// 事务管理
	ITransactional bool
}

// DefaultServiceConfig 默认服务配置
func DefaultServiceConfig() *ServiceConfig {
	return &ServiceConfig{
		AutoValidate:   true,
		AutoTimestamp:  true,
		SoftDelete:     false,
		AuditFields:    false,
		MaxBatchSize:   1000,
		MaxPageSize:    1000,
		EnableCache:    false,
		CacheTTL:       300, // 5 minutes
		EnableAudit:    false,
		OptimisticLock: false,
		ITransactional: false,
	}
}

// Application 应用服务实现
type Application[T domain.IEntity[int64]] struct {
	*crud.CRUDService[T, int64]
	validator validation.IValidator
	config    *ServiceConfig
}

// NewApplication 创建应用服务
func NewApplication[T domain.IEntity[int64]](
	repository crud.IRepository[T, int64],
	validator validation.IValidator,
	config *ServiceConfig,
) IApplication[T] {
	if config == nil {
		config = DefaultServiceConfig()
	}

	return &Application[T]{
		CRUDService: crud.NewCRUDService[T, int64](repository),
		validator:   validator,
		config:      config,
	}
}

// GetConfig 获取服务配置
func (s *Application[T]) GetConfig() *ServiceConfig {
	return s.config
}

// UpdateConfig 更新服务配置
func (s *Application[T]) UpdateConfig(config *ServiceConfig) {
	if config == nil {
		return
	}
	copyCfg := *config
	s.config = &copyCfg
}

// SetValidator 设置验证器
func (s *Application[T]) SetValidator(v validation.IValidator) {
	s.validator = v
}

// Create 创建实体（带生命周期钩子）
func (s *Application[T]) Create(ctx context.Context, entity T) error {
	// 执行前置操作
	if err := s.BeforeCreate(ctx, entity); err != nil {
		return err
	}

	// 执行创建
	if err := s.CRUDService.Create(ctx, entity); err != nil {
		return err
	}

	// 执行后置操作
	return s.AfterCreate(ctx, entity)
}

// Update 更新实体（带生命周期钩子）
func (s *Application[T]) Update(ctx context.Context, entity T) error {
	// 执行前置操作
	if err := s.BeforeUpdate(ctx, entity); err != nil {
		return err
	}

	// 执行更新
	if err := s.CRUDService.Update(ctx, entity); err != nil {
		return err
	}

	// 执行后置操作
	return s.AfterUpdate(ctx, entity)
}

// Delete 删除实体（带生命周期钩子）
func (s *Application[T]) Delete(ctx context.Context, id int64) error {
	// 执行前置操作
	if err := s.BeforeDelete(ctx, id); err != nil {
		return err
	}

	// 执行删除
	if err := s.CRUDService.Delete(ctx, id); err != nil {
		return err
	}

	// 执行后置操作
	return s.AfterDelete(ctx, id)
}

// 生命周期钩子方法（可被具体实现覆盖）
func (s *Application[T]) Validate(entity T) error {
	if s.config.AutoValidate && s.validator != nil {
		return s.validator.Validate(entity)
	}
	return nil
}

func (s *Application[T]) BeforeCreate(ctx context.Context, entity T) error { return nil }
func (s *Application[T]) AfterCreate(ctx context.Context, entity T) error  { return nil }
func (s *Application[T]) BeforeUpdate(ctx context.Context, entity T) error { return nil }
func (s *Application[T]) AfterUpdate(ctx context.Context, entity T) error  { return nil }
func (s *Application[T]) BeforeDelete(ctx context.Context, id int64) error { return nil }
func (s *Application[T]) AfterDelete(ctx context.Context, id int64) error  { return nil }
