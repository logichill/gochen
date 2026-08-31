// Package crud 提供通用 CRUD 应用服务（用例编排）模板。
package crud

import (
	"context"
	"gochen/app/internal/writeflow"
	"gochen/app/query"
	"gochen/domain"
	"gochen/domain/crud"
	"gochen/errors"
	"gochen/validate"
	"sync"
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
	repository  crud.IRepository[T, ID]
	stateMu     sync.RWMutex
	validator   validate.IValidator
	config      *ServiceConfig
	hooks       *Hooks[T, ID]
	batchWriter *BatchWriter[T, ID]
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
	copyCfg := *config
	normalizeServiceConfig(&copyCfg)

	app := &Application[T, ID]{
		repository: repository,
		validator:  validator,
		config:     &copyCfg,
	}
	app.batchWriter = NewBatchWriter(app)
	return app, nil
}

func (s *Application[T, ID]) Config() *ServiceConfig {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return cloneServiceConfig(s.config)
}

// UpdateConfig 更新服务配置（会复制一份，避免外部后续修改影响运行中实例）。
func (s *Application[T, ID]) UpdateConfig(config *ServiceConfig) {
	if config == nil {
		return
	}
	copyCfg := *config
	normalizeServiceConfig(&copyCfg)
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	s.config = &copyCfg
}

// SetValidator 设置实体校验器（用于写入前校验）。
func (s *Application[T, ID]) SetValidator(v validate.IValidator) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	s.validator = v
}

// SetHooks 设置生命周期钩子（用于在 CRUD 写入前后扩展业务逻辑）。
func (s *Application[T, ID]) SetHooks(h *Hooks[T, ID]) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	s.hooks = h
}

func runBeforeCreateHook[T domain.IEntity[ID], ID comparable](hooks *Hooks[T, ID], ctx context.Context, entity T) error {
	if hooks == nil || hooks.BeforeCreate == nil {
		return nil
	}
	return hooks.BeforeCreate(ctx, entity)
}

func runAfterCreateHook[T domain.IEntity[ID], ID comparable](hooks *Hooks[T, ID], ctx context.Context, entity T) error {
	if hooks == nil || hooks.AfterCreate == nil {
		return nil
	}
	return hooks.AfterCreate(ctx, entity)
}

func postCommitCreateHook[T domain.IEntity[ID], ID comparable](hooks *Hooks[T, ID], entity T) func(context.Context) error {
	if hooks == nil || hooks.PostCommitCreate == nil {
		return nil
	}
	return func(ctx context.Context) error {
		return hooks.PostCommitCreate(ctx, entity)
	}
}

func runBeforeUpdateHook[T domain.IEntity[ID], ID comparable](hooks *Hooks[T, ID], ctx context.Context, entity T) error {
	if hooks == nil || hooks.BeforeUpdate == nil {
		return nil
	}
	return hooks.BeforeUpdate(ctx, entity)
}

func runAfterUpdateHook[T domain.IEntity[ID], ID comparable](hooks *Hooks[T, ID], ctx context.Context, entity T) error {
	if hooks == nil || hooks.AfterUpdate == nil {
		return nil
	}
	return hooks.AfterUpdate(ctx, entity)
}

func postCommitUpdateHook[T domain.IEntity[ID], ID comparable](hooks *Hooks[T, ID], entity T) func(context.Context) error {
	if hooks == nil || hooks.PostCommitUpdate == nil {
		return nil
	}
	return func(ctx context.Context) error {
		return hooks.PostCommitUpdate(ctx, entity)
	}
}

func runBeforeDeleteHook[T domain.IEntity[ID], ID comparable](hooks *Hooks[T, ID], ctx context.Context, id ID) error {
	if hooks == nil || hooks.BeforeDelete == nil {
		return nil
	}
	return hooks.BeforeDelete(ctx, id)
}

func runAfterDeleteHook[T domain.IEntity[ID], ID comparable](hooks *Hooks[T, ID], ctx context.Context, id ID) error {
	if hooks == nil || hooks.AfterDelete == nil {
		return nil
	}
	return hooks.AfterDelete(ctx, id)
}

func postCommitDeleteHook[T domain.IEntity[ID], ID comparable](hooks *Hooks[T, ID], id ID) func(context.Context) error {
	if hooks == nil || hooks.PostCommitDelete == nil {
		return nil
	}
	return func(ctx context.Context) error {
		return hooks.PostCommitDelete(ctx, id)
	}
}

// runBeforeCreate 执行显式配置的 BeforeCreate hook；未配置则 no-op。
func (s *Application[T, ID]) runBeforeCreate(ctx context.Context, entity T) error {
	return runBeforeCreateHook(s.hooksSnapshot(), ctx, entity)
}

// runAfterCreate 执行显式配置的 AfterCreate hook；未配置则 no-op。
func (s *Application[T, ID]) runAfterCreate(ctx context.Context, entity T) error {
	return runAfterCreateHook(s.hooksSnapshot(), ctx, entity)
}

func (s *Application[T, ID]) postCommitCreate(entity T) func(context.Context) error {
	return postCommitCreateHook(s.hooksSnapshot(), entity)
}

// runBeforeUpdate 执行显式配置的 BeforeUpdate hook；未配置则 no-op。
func (s *Application[T, ID]) runBeforeUpdate(ctx context.Context, entity T) error {
	return runBeforeUpdateHook(s.hooksSnapshot(), ctx, entity)
}

// runAfterUpdate 执行显式配置的 AfterUpdate hook；未配置则 no-op。
func (s *Application[T, ID]) runAfterUpdate(ctx context.Context, entity T) error {
	return runAfterUpdateHook(s.hooksSnapshot(), ctx, entity)
}

func (s *Application[T, ID]) postCommitUpdate(entity T) func(context.Context) error {
	return postCommitUpdateHook(s.hooksSnapshot(), entity)
}

// runBeforeDelete 执行显式配置的 BeforeDelete hook；未配置则 no-op。
func (s *Application[T, ID]) runBeforeDelete(ctx context.Context, id ID) error {
	return runBeforeDeleteHook(s.hooksSnapshot(), ctx, id)
}

// runAfterDelete 执行显式配置的 AfterDelete hook；未配置则 no-op。
func (s *Application[T, ID]) runAfterDelete(ctx context.Context, id ID) error {
	return runAfterDeleteHook(s.hooksSnapshot(), ctx, id)
}

func (s *Application[T, ID]) postCommitDelete(id ID) func(context.Context) error {
	return postCommitDeleteHook(s.hooksSnapshot(), id)
}

func (s *Application[T, ID]) postCommitCreateCallbacks(entities []T) []func(context.Context) error {
	hooks := s.hooksSnapshot()
	return writeflow.CallbacksFor(entities, func(entity T) func(context.Context) error {
		return postCommitCreateHook(hooks, entity)
	})
}

func (s *Application[T, ID]) postCommitUpdateCallbacks(entities []T) []func(context.Context) error {
	hooks := s.hooksSnapshot()
	return writeflow.CallbacksFor(entities, func(entity T) func(context.Context) error {
		return postCommitUpdateHook(hooks, entity)
	})
}

func (s *Application[T, ID]) postCommitDeleteCallbacks(ids []ID) []func(context.Context) error {
	hooks := s.hooksSnapshot()
	return writeflow.CallbacksFor(ids, func(id ID) func(context.Context) error {
		return postCommitDeleteHook(hooks, id)
	})
}

func (s *Application[T, ID]) transactionalRepository() (ITransactional, bool) {
	txRepo, ok := s.repository.(ITransactional)
	return txRepo, ok
}

// Create 创建实体，执行完整的生命周期钩子。
func (s *Application[T, ID]) Create(ctx context.Context, entity T) error {
	hooks := s.hooksSnapshot()
	return s.runWriteFlow(ctx, writeflow.Plan{
		Before: func(writeCtx context.Context) error {
			return runBeforeCreateHook(hooks, writeCtx, entity)
		},
		Validate: func(context.Context) error {
			return s.Validate(entity)
		},
		Write: func(writeCtx context.Context) error {
			return s.repository.Create(writeCtx, entity)
		},
		After: func(writeCtx context.Context) error {
			return runAfterCreateHook(hooks, writeCtx, entity)
		},
		PostCommits:     writeflow.PostCommits(postCommitCreateHook(hooks, entity)),
		CallbackContext: ctx,
	})
}

// Update 更新实体，执行完整的生命周期钩子。
func (s *Application[T, ID]) Update(ctx context.Context, entity T) error {
	hooks := s.hooksSnapshot()
	return s.runWriteFlow(ctx, writeflow.Plan{
		Before: func(writeCtx context.Context) error {
			return runBeforeUpdateHook(hooks, writeCtx, entity)
		},
		Validate: func(context.Context) error {
			return s.Validate(entity)
		},
		Write: func(writeCtx context.Context) error {
			return s.repository.Update(writeCtx, entity)
		},
		After: func(writeCtx context.Context) error {
			return runAfterUpdateHook(hooks, writeCtx, entity)
		},
		PostCommits:     writeflow.PostCommits(postCommitUpdateHook(hooks, entity)),
		CallbackContext: ctx,
	})
}

// Delete 删除实体，执行完整的生命周期钩子。
func (s *Application[T, ID]) Delete(ctx context.Context, id ID) error {
	hooks := s.hooksSnapshot()
	return s.runWriteFlow(ctx, writeflow.Plan{
		Before: func(writeCtx context.Context) error {
			return runBeforeDeleteHook(hooks, writeCtx, id)
		},
		Write: func(writeCtx context.Context) error {
			return s.repository.Delete(writeCtx, id)
		},
		After: func(writeCtx context.Context) error {
			return runAfterDeleteHook(hooks, writeCtx, id)
		},
		PostCommits:     writeflow.PostCommits(postCommitDeleteHook(hooks, id)),
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

// CreateAll 批量创建实体。
func (s *Application[T, ID]) CreateAll(ctx context.Context, entities []T) error {
	return s.batchWriter.CreateAll(ctx, entities)
}

// UpdateAll 批量更新实体。
func (s *Application[T, ID]) UpdateAll(ctx context.Context, entities []T) error {
	return s.batchWriter.UpdateAll(ctx, entities)
}

// DeleteAll 批量删除实体。
func (s *Application[T, ID]) DeleteAll(ctx context.Context, ids []ID) error {
	return s.batchWriter.DeleteAll(ctx, ids)
}

// QueryRepository 返回底层查询仓储扩展（若支持）。
func (s *Application[T, ID]) QueryRepository() (crud.IQueryRepository[T, ID], bool) {
	repo, ok := s.repository.(crud.IQueryRepository[T, ID])
	return repo, ok
}

// Validate 先校验实体领域不变量，再执行可注入的应用策略校验。
func (s *Application[T, ID]) Validate(entity T) error {
	cfg := s.serviceConfig()
	if !cfg.AutoValidate {
		return nil
	}
	if validatable, ok := any(entity).(domain.IValidatable); ok {
		if err := validatable.Validate(); err != nil {
			return err
		}
	}
	if validator := s.validatorSnapshot(); validator != nil {
		return validator.Validate(entity)
	}
	return nil
}

func cloneServiceConfig(config *ServiceConfig) *ServiceConfig {
	if config == nil {
		return nil
	}
	copyCfg := *config
	normalizeServiceConfig(&copyCfg)
	return &copyCfg
}

func normalizeServiceConfig(config *ServiceConfig) {
	if config == nil {
		return
	}
	if config.MaxBatchSize <= 0 {
		config.MaxBatchSize = defaultMaxBatchSize
	}
	if config.MaxPageSize <= 0 {
		config.MaxPageSize = defaultMaxPageSize
	}
}

func (s *Application[T, ID]) serviceConfig() ServiceConfig {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	if s.config == nil {
		return *DefaultServiceConfig()
	}
	cfg := *s.config
	normalizeServiceConfig(&cfg)
	return cfg
}

func (s *Application[T, ID]) maxBatchSize() int {
	cfg := s.serviceConfig()
	return cfg.MaxBatchSize
}

func (s *Application[T, ID]) maxPageSize() int {
	cfg := s.serviceConfig()
	return cfg.MaxPageSize
}

func (s *Application[T, ID]) validatorSnapshot() validate.IValidator {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.validator
}

func (s *Application[T, ID]) hooksSnapshot() *Hooks[T, ID] {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.hooks
}
