// Package api 提供 RESTful API 构建器
package api

import (
	"fmt"

	"gochen/app/application"
	"gochen/domain"
	"gochen/http"
	"gochen/validation"
)

type configurableService interface {
	UpdateConfig(*application.ServiceConfig)
	GetConfig() *application.ServiceConfig
}

type validatorAware interface {
	SetValidator(validation.IValidator)
}

func cloneServiceConfig(cfg *application.ServiceConfig) *application.ServiceConfig {
	if cfg == nil {
		def := application.DefaultServiceConfig()
		copyCfg := *def
		return &copyCfg
	}
	clone := *cfg
	return &clone
}

// IApiBuilder RESTful API 构建器接口
type IApiBuilder[T domain.IEntity[int64]] interface {
	// 配置路由
	Route(config func(*RouteConfig)) IApiBuilder[T]

	// 配置服务
	Service(config func(*application.ServiceConfig)) IApiBuilder[T]

	// 添加中间件
	Middleware(middlewares ...http.Middleware) IApiBuilder[T]

	// 构建并注册
	Build(group http.IRouteGroup) error
}

// ApiBuilder RESTful API 构建器实现
type ApiBuilder[T domain.IEntity[int64]] struct {
	routeConfig   *RouteConfig
	serviceConfig *application.ServiceConfig
	middlewares   []http.Middleware
	service       application.IApplication[T]
	validator     validation.IValidator
}

// NewApiBuilder 创建 RESTful 构建器
func NewApiBuilder[T domain.IEntity[int64]](
	svc application.IApplication[T],
	validator validation.IValidator,
) *ApiBuilder[T] {
	routeCfg := DefaultRouteConfig()
	if validator != nil && routeCfg.Validator == nil {
		routeCfg.Validator = func(value any) error {
			return validator.Validate(value)
		}
	}

	var svcConfig *application.ServiceConfig
	if configurable, ok := any(svc).(configurableService); ok {
		svcConfig = cloneServiceConfig(configurable.GetConfig())
	} else {
		svcConfig = cloneServiceConfig(application.DefaultServiceConfig())
	}

	return &ApiBuilder[T]{
		routeConfig:   routeCfg,
		serviceConfig: svcConfig,
		service:       svc,
		validator:     validator,
	}
}

// Route 配置路由
func (rb *ApiBuilder[T]) Route(config func(*RouteConfig)) IApiBuilder[T] {
	config(rb.routeConfig)
	return rb
}

// Service 配置服务
func (rb *ApiBuilder[T]) Service(config func(*application.ServiceConfig)) IApiBuilder[T] {
	if config == nil {
		return rb
	}
	if rb.serviceConfig == nil {
		rb.serviceConfig = cloneServiceConfig(application.DefaultServiceConfig())
	}
	config(rb.serviceConfig)
	return rb
}

// Middleware 添加中间件
func (rb *ApiBuilder[T]) Middleware(middlewares ...http.Middleware) IApiBuilder[T] {
	rb.middlewares = append(rb.middlewares, middlewares...)
	return rb
}

// Build 构建并注册
func (rb *ApiBuilder[T]) Build(group http.IRouteGroup) error {
	// 重新创建服务（如果配置有变化）
	if rb.service == nil {
		return fmt.Errorf("service cannot be nil")
	}

	if rb.serviceConfig != nil {
		if svc, ok := any(rb.service).(configurableService); ok {
			svc.UpdateConfig(cloneServiceConfig(rb.serviceConfig))
		}
	}

	if rb.validator != nil {
		if svc, ok := any(rb.service).(validatorAware); ok {
			svc.SetValidator(rb.validator)
		}
	}

	// 创建路由构建器
	routeBuilder := NewRouteBuilder(rb.service).
		WithConfig(rb.routeConfig)

	// 添加中间件
	if len(rb.middlewares) > 0 {
		routeBuilder.Use(rb.middlewares...)
	}

	// 注册到路由组
	return routeBuilder.Register(group)
}

// 便捷函数
func RegisterRESTfulAPI[T domain.IEntity[int64]](
	group http.IRouteGroup,
	svc application.IApplication[T],
	validator validation.IValidator,
	options ...func(*ApiBuilder[T]),
) error {
	builder := NewApiBuilder(svc, validator)

	// 应用选项
	for _, option := range options {
		option(builder)
	}

	return builder.Build(group)
}

// 预定义配置选项
func WithBatchOperations[T domain.IEntity[int64]](maxSize int) func(*ApiBuilder[T]) {
	return func(rb *ApiBuilder[T]) {
		rb.Route(func(config *RouteConfig) {
			config.EnableBatch = true
			config.MaxPageSize = maxSize
		})
	}
}

func WithPagination[T domain.IEntity[int64]](defaultSize, maxSize int) func(*ApiBuilder[T]) {
	return func(rb *ApiBuilder[T]) {
		rb.Route(func(config *RouteConfig) {
			config.EnablePagination = true
			config.DefaultPageSize = defaultSize
			config.MaxPageSize = maxSize
		})
	}
}

func WithCustomValidator[T domain.IEntity[int64]](validator func(any) error) func(*ApiBuilder[T]) {
	return func(rb *ApiBuilder[T]) {
		rb.Route(func(config *RouteConfig) {
			config.Validator = validator
		})
	}
}

func WithCORS[T domain.IEntity[int64]](origins []string) func(*ApiBuilder[T]) {
	return func(rb *ApiBuilder[T]) {
		rb.Route(func(config *RouteConfig) {
			if config.CORS != nil {
				config.CORS.AllowOrigins = origins
			}
		})
	}
}

func WithAuth[T domain.IEntity[int64]](middleware http.Middleware) func(*ApiBuilder[T]) {
	return func(rb *ApiBuilder[T]) {
		rb.Middleware(middleware)
	}
}

func WithLogging[T domain.IEntity[int64]](middleware http.Middleware) func(*ApiBuilder[T]) {
	return func(rb *ApiBuilder[T]) {
		rb.Middleware(middleware)
	}
}
