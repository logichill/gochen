// Package crud 提供通用 CRUD 应用服务（用例编排）模板。
package crud

import (
	"gochen/domain"
	"gochen/domain/crud"
	"gochen/errors"
	"gochen/validate"
)

// Application 应用服务实现。
type Application[T domain.IEntity[ID], ID comparable] struct {
	repository crud.IRepository[T, ID]
	validator  validate.IValidator
	config     *ServiceConfig
	hooks      Hooks[T, ID]
	flows      flows[T, ID]
}

var _ ICapabilities = (*Application[domain.IEntity[int64], int64])(nil)

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
		app.hooks = *opts.Hooks
	}
	app.flows = app.buildFlows()
	return app, nil
}

// Repository 返回组合根装配所需的底层仓储。
func (s *Application[T, ID]) Repository() crud.IRepository[T, ID] { return s.repository }

// Config 返回构造期应用配置的副本。
func (s *Application[T, ID]) Config() *ServiceConfig {
	return cloneServiceConfig(s.config)
}

// Hooks 返回构造期钩子快照（值拷贝）。
func (s *Application[T, ID]) Hooks() Hooks[T, ID] { return s.hooks }

func (s *Application[T, ID]) serviceConfig() ServiceConfig {
	if s.config == nil {
		return *DefaultServiceConfig()
	}
	cfg := *s.config
	normalizeServiceConfig(&cfg)
	return cfg
}
