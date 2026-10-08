// Package crud 提供通用 CRUD 应用服务（用例编排）模板。
package crud

import (
	"context"
	"gochen/app/internal/writeflow"

	"gochen/domain"
	"gochen/domain/crud"
	"gochen/errors"
	"gochen/validate"
)

// Application 应用服务实现。
type Application[T domain.IEntity[ID], ID comparable] struct {
	repository  crud.IRepository[T, ID]
	validator   validate.IValidator
	config      *ServiceConfig
	hooks       *Hooks[T, ID]
	batchWriter *BatchWriter[T, ID]
}

// NewApplication 创建应用服务实例。
//
// 参数：
// - repository：实体仓储（承载 CRUD 的实际读写与查询）。
// - options：构造期配置（nil 使用默认值）；配置和钩子均复制后持有。
//
// 返回：
// - err：repository 为空等输入错误会返回 err。
func NewApplication[T domain.IEntity[ID], ID comparable](
	repository crud.IRepository[T, ID],
	options *Options[T, ID],
) (*Application[T, ID], error) {
	if repository == nil {
		return nil, errors.NewCode(errors.InvalidInput, "repository cannot be nil")
	}
	var opts Options[T, ID]
	if options != nil {
		opts = *options
	}
	if opts.Config == nil {
		opts.Config = DefaultServiceConfig()
	}
	copyCfg := *opts.Config
	normalizeServiceConfig(&copyCfg)

	app := &Application[T, ID]{
		repository: repository,
		validator:  opts.Validator,
		config:     &copyCfg,
	}
	if opts.Hooks != nil {
		hooks := *opts.Hooks
		app.hooks = &hooks
	}
	app.batchWriter = NewBatchWriter(app)
	return app, nil
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

// Count 返回当前实体的总记录数。
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

func (s *Application[T, ID]) serviceConfig() ServiceConfig {
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
	return s.validator
}

func (s *Application[T, ID]) hooksSnapshot() *Hooks[T, ID] {
	return s.hooks
}
