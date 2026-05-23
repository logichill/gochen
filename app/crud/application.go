// Package crud 提供通用 CRUD 应用服务（用例编排）模板。
package crud

import (
	"context"
	"gochen/app/internal/writeflow"
	"gochen/db/query"
	"gochen/domain"
	"gochen/domain/crud"
	"gochen/errors"
	"gochen/validate"
)

// IReader 表示 CRUD 场景下的读能力集合。
type IReader[T domain.IEntity[ID], ID comparable] interface {
	Get(ctx context.Context, id ID) (T, error)
	List(ctx context.Context, offset, limit int) ([]T, error)
	Count(ctx context.Context) (int64, error)
	Exists(ctx context.Context, id ID) (bool, error)
	ListByQuery(ctx context.Context, query *query.QueryRequest) ([]T, error)
	ListPage(ctx context.Context, request *query.PageRequest) (*query.PagedResult[T], error)
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

// IRepositoryProvider 暴露底层仓储。
type IRepositoryProvider[T domain.IEntity[ID], ID comparable] interface {
	Repository() crud.IRepository[T, ID]
}

// IQueryRepositoryProvider 暴露底层查询仓储扩展。
type IQueryRepositoryProvider[T domain.IEntity[ID], ID comparable] interface {
	QueryRepository() (crud.IQueryRepository[T, ID], bool)
}

// IEntityValidator 定义实体校验能力接口。
type IEntityValidator[T domain.IEntity[ID], ID comparable] interface {
	Validate(entity T) error
}

// IConfigProvider 暴露服务配置。
type IConfigProvider interface {
	Config() *ServiceConfig
}

// IApplication 抽象Application能力接口。
type IApplication[T domain.IEntity[ID], ID comparable] interface {
	IReader[T, ID]
	IWriter[T, ID]
	IRepositoryProvider[T, ID]
	IQueryRepositoryProvider[T, ID]
	IEntityValidator[T, ID]
	IConfigProvider
}

// ServiceConfig 服务配置。
type ServiceConfig struct {
	// 自动验证
	AutoValidate bool

	// 最大批量操作数量
	MaxBatchSize int

	// 最大单页大小（分页查询）
	MaxPageSize int
}

const (
	defaultMaxBatchSize = 1000
	defaultMaxPageSize  = 1000
)

func DefaultServiceConfig() *ServiceConfig {
	return &ServiceConfig{
		AutoValidate: true,
		MaxBatchSize: defaultMaxBatchSize,
		MaxPageSize:  defaultMaxPageSize,
	}
}

// Application 应用服务实现。
type Application[T domain.IEntity[ID], ID comparable] struct {
	repository crud.IRepository[T, ID]
	validator  validate.IValidator
	config     *ServiceConfig
	hooks      *Hooks[T, ID]
}

// IServiceConfigUpdatable 抽象服务配置Updatable能力接口。
type IServiceConfigUpdatable interface {
	UpdateConfig(*ServiceConfig)
}

// IValidatorAware 抽象ValidatorAware能力接口。
type IValidatorAware interface {
	SetValidator(validate.IValidator)
}

// IHooksAware 抽象钩子集合Aware能力接口。
type IHooksAware[T domain.IEntity[ID], ID comparable] interface {
	SetHooks(*Hooks[T, ID])
}

// RunBeforeDelete 执行显式 Hooks.BeforeDelete；未配置则 no-op。
func (s *Application[T, ID]) RunBeforeDelete(ctx context.Context, id ID) error {
	return s.runBeforeDelete(ctx, id)
}

// RunAfterDelete 执行显式 Hooks.AfterDelete；未配置则 no-op。
func (s *Application[T, ID]) RunAfterDelete(ctx context.Context, id ID) error {
	return s.runAfterDelete(ctx, id)
}

// NewApplication 创建应用服务实例。
//
// 参数：
// - repository：实体仓储（承载 CRUD 的实际读写与查询）。
// - validator：可选校验器（配合 AutoValidate 在写入前执行实体校验）。
// - config：服务配置（nil 表示使用默认配置）。
//
// 返回：
// - err：repository 为空等输入错误会返回 err。
func NewApplication[T domain.IEntity[ID], ID comparable](
	repository crud.IRepository[T, ID],
	validator validate.IValidator,
	config *ServiceConfig,
) (*Application[T, ID], error) {
	if repository == nil {
		return nil, errors.NewCode(errors.InvalidInput, "repository cannot be nil")
	}
	if config == nil {
		config = DefaultServiceConfig()
	}

	return &Application[T, ID]{
		repository: repository,
		validator:  validator,
		config:     config,
	}, nil
}

func (s *Application[T, ID]) Config() *ServiceConfig {
	return s.config
}

// UpdateConfig 更新服务配置（会复制一份，避免外部后续修改影响运行中实例）。
func (s *Application[T, ID]) UpdateConfig(config *ServiceConfig) {
	if config == nil {
		return
	}
	copyCfg := *config
	s.config = &copyCfg
}

// SetValidator 设置实体校验器（用于写入前校验）。
func (s *Application[T, ID]) SetValidator(v validate.IValidator) {
	s.validator = v
}

// SetHooks 设置生命周期钩子（用于在 CRUD 写入前后扩展业务逻辑）。
func (s *Application[T, ID]) SetHooks(h *Hooks[T, ID]) {
	s.hooks = h
}

// runBeforeCreate 执行显式配置的 BeforeCreate hook；未配置则 no-op。
func (s *Application[T, ID]) runBeforeCreate(ctx context.Context, entity T) error {
	if s.hooks == nil || s.hooks.BeforeCreate == nil {
		return nil
	}
	return s.hooks.BeforeCreate(ctx, entity)
}

// runAfterCreate 执行显式配置的 AfterCreate hook；未配置则 no-op。
func (s *Application[T, ID]) runAfterCreate(ctx context.Context, entity T) error {
	if s.hooks == nil || s.hooks.AfterCreate == nil {
		return nil
	}
	return s.hooks.AfterCreate(ctx, entity)
}

func (s *Application[T, ID]) postCommitCreate(entity T) func(context.Context) error {
	if s.hooks == nil || s.hooks.PostCommitCreate == nil {
		return nil
	}
	return func(ctx context.Context) error {
		return s.hooks.PostCommitCreate(ctx, entity)
	}
}

// runBeforeUpdate 执行显式配置的 BeforeUpdate hook；未配置则 no-op。
func (s *Application[T, ID]) runBeforeUpdate(ctx context.Context, entity T) error {
	if s.hooks == nil || s.hooks.BeforeUpdate == nil {
		return nil
	}
	return s.hooks.BeforeUpdate(ctx, entity)
}

// runAfterUpdate 执行显式配置的 AfterUpdate hook；未配置则 no-op。
func (s *Application[T, ID]) runAfterUpdate(ctx context.Context, entity T) error {
	if s.hooks == nil || s.hooks.AfterUpdate == nil {
		return nil
	}
	return s.hooks.AfterUpdate(ctx, entity)
}

func (s *Application[T, ID]) postCommitUpdate(entity T) func(context.Context) error {
	if s.hooks == nil || s.hooks.PostCommitUpdate == nil {
		return nil
	}
	return func(ctx context.Context) error {
		return s.hooks.PostCommitUpdate(ctx, entity)
	}
}

// runBeforeDelete 执行显式配置的 BeforeDelete hook；未配置则 no-op。
func (s *Application[T, ID]) runBeforeDelete(ctx context.Context, id ID) error {
	if s.hooks == nil || s.hooks.BeforeDelete == nil {
		return nil
	}
	return s.hooks.BeforeDelete(ctx, id)
}

// runAfterDelete 执行显式配置的 AfterDelete hook；未配置则 no-op。
func (s *Application[T, ID]) runAfterDelete(ctx context.Context, id ID) error {
	if s.hooks == nil || s.hooks.AfterDelete == nil {
		return nil
	}
	return s.hooks.AfterDelete(ctx, id)
}

func (s *Application[T, ID]) postCommitDelete(id ID) func(context.Context) error {
	if s.hooks == nil || s.hooks.PostCommitDelete == nil {
		return nil
	}
	return func(ctx context.Context) error {
		return s.hooks.PostCommitDelete(ctx, id)
	}
}

func (s *Application[T, ID]) postCommitCreateCallbacks(entities []T) []func(context.Context) error {
	callbacks := make([]func(context.Context) error, 0, len(entities))
	for _, entity := range entities {
		if cb := s.postCommitCreate(entity); cb != nil {
			callbacks = append(callbacks, cb)
		}
	}
	return callbacks
}

func (s *Application[T, ID]) postCommitUpdateCallbacks(entities []T) []func(context.Context) error {
	callbacks := make([]func(context.Context) error, 0, len(entities))
	for _, entity := range entities {
		if cb := s.postCommitUpdate(entity); cb != nil {
			callbacks = append(callbacks, cb)
		}
	}
	return callbacks
}

func (s *Application[T, ID]) postCommitDeleteCallbacks(ids []ID) []func(context.Context) error {
	callbacks := make([]func(context.Context) error, 0, len(ids))
	for _, id := range ids {
		if cb := s.postCommitDelete(id); cb != nil {
			callbacks = append(callbacks, cb)
		}
	}
	return callbacks
}

func (s *Application[T, ID]) transactionalRepository() (ITransactional, bool) {
	txRepo, ok := s.repository.(ITransactional)
	return txRepo, ok
}

// Create 创建实体，执行完整的生命周期钩子。
func (s *Application[T, ID]) Create(ctx context.Context, entity T) error {
	return s.runWriteFlow(ctx, writeflow.Plan{
		Before: func(writeCtx context.Context) error {
			return s.runBeforeCreate(writeCtx, entity)
		},
		Validate: func(context.Context) error {
			return s.Validate(entity)
		},
		Write: func(writeCtx context.Context) error {
			return s.repository.Create(writeCtx, entity)
		},
		After: func(writeCtx context.Context) error {
			return s.runAfterCreate(writeCtx, entity)
		},
		PostCommits:     callbacksToPostCommits([]func(context.Context) error{s.postCommitCreate(entity)}),
		CallbackContext: ctx,
	})
}

// Update 更新实体，执行完整的生命周期钩子。
func (s *Application[T, ID]) Update(ctx context.Context, entity T) error {
	return s.runWriteFlow(ctx, writeflow.Plan{
		Before: func(writeCtx context.Context) error {
			return s.runBeforeUpdate(writeCtx, entity)
		},
		Validate: func(context.Context) error {
			return s.Validate(entity)
		},
		Write: func(writeCtx context.Context) error {
			return s.repository.Update(writeCtx, entity)
		},
		After: func(writeCtx context.Context) error {
			return s.runAfterUpdate(writeCtx, entity)
		},
		PostCommits:     callbacksToPostCommits([]func(context.Context) error{s.postCommitUpdate(entity)}),
		CallbackContext: ctx,
	})
}

// Delete 删除实体，执行完整的生命周期钩子。
func (s *Application[T, ID]) Delete(ctx context.Context, id ID) error {
	return s.runWriteFlow(ctx, writeflow.Plan{
		Before: func(writeCtx context.Context) error {
			return s.runBeforeDelete(writeCtx, id)
		},
		Write: func(writeCtx context.Context) error {
			return s.repository.Delete(writeCtx, id)
		},
		After: func(writeCtx context.Context) error {
			return s.runAfterDelete(writeCtx, id)
		},
		PostCommits:     callbacksToPostCommits([]func(context.Context) error{s.postCommitDelete(id)}),
		CallbackContext: ctx,
	})
}

// Get 根据 ID 获取实体。
func (s *Application[T, ID]) Get(ctx context.Context, id ID) (T, error) {
	return s.repository.Get(ctx, id)
}

// List 返回实体列表，支持分页。
func (s *Application[T, ID]) List(ctx context.Context, offset, limit int) ([]T, error) {
	repo, ok := s.QueryRepository()
	if !ok {
		return nil, errors.NewCode(errors.Unsupported, "list requires repository to implement crud.IQueryRepository")
	}
	return repo.List(ctx, offset, limit)
}

func (s *Application[T, ID]) Count(ctx context.Context) (int64, error) {
	repo, ok := s.QueryRepository()
	if !ok {
		return 0, errors.NewCode(errors.Unsupported, "count requires repository to implement crud.IQueryRepository")
	}
	return repo.Count(ctx)
}

// Exists 判断指定 ID 的实体是否存在。
func (s *Application[T, ID]) Exists(ctx context.Context, id ID) (bool, error) {
	repo, ok := s.QueryRepository()
	if !ok {
		return false, errors.NewCode(errors.Unsupported, "exists requires repository to implement crud.IQueryRepository")
	}
	return repo.Exists(ctx, id)
}

func (s *Application[T, ID]) Repository() crud.IRepository[T, ID] { return s.repository }

// QueryRepository 返回底层查询仓储扩展（若支持）。
func (s *Application[T, ID]) QueryRepository() (crud.IQueryRepository[T, ID], bool) {
	repo, ok := s.repository.(crud.IQueryRepository[T, ID])
	return repo, ok
}

// Validate 校验实体是否满足约束条件。
func (s *Application[T, ID]) Validate(entity T) error {
	if s.config.AutoValidate && s.validator != nil {
		return s.validator.Validate(entity)
	}
	return nil
}
