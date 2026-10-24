package crud

import (
	"gochen/domain"
	"gochen/validate"
)

// Options 在构造时确定应用行为；nil Config 使用 DefaultServiceConfig。
// Config 和 Hooks 会被复制；Validator 及钩子闭包引用的对象由调用方保证并发安全。
type Options[T domain.IEntity[ID], ID comparable] struct {
	Config    *ServiceConfig
	Validator validate.IValidator
	Hooks     *Hooks[T, ID]
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

func cloneServiceConfig(config *ServiceConfig) *ServiceConfig {
	if config == nil {
		return nil
	}
	copyCfg := *config
	normalizeServiceConfig(&copyCfg)
	return &copyCfg
}
