package api

import (
	"fmt"

	app "gochen/app"
	"gochen/domain/entity"
	sservice "gochen/domain/service"
	core "gochen/httpx"
	validation "gochen/validation"
)

// IAuditedApiBuilder 面向审计型业务的构建器接口
type IAuditedApiBuilder[T entity.Entity[int64]] interface {
	Route(config func(*RouteConfig)) IAuditedApiBuilder[T]
	Service(config func(*app.ServiceConfig)) IAuditedApiBuilder[T]
	Middleware(middlewares ...core.Middleware) IAuditedApiBuilder[T]
	Build(group core.IRouteGroup) error
}

type auditedConfigurableService interface {
	UpdateConfig(*app.ServiceConfig)
	GetConfig() *app.ServiceConfig
}
type auditedValidatorAware interface{ SetValidator(validation.IValidator) }

// AuditedApiBuilder 构建器实现
type AuditedApiBuilder[T entity.Entity[int64]] struct {
	routeConfig   *RouteConfig
	serviceConfig *app.ServiceConfig
	middlewares   []core.Middleware
	service       sservice.IAuditedService[T, int64]
	validator     validation.IValidator
}

func NewAuditedApiBuilder[T entity.Entity[int64]](svc sservice.IAuditedService[T, int64], validator validation.IValidator) *AuditedApiBuilder[T] {
	rc := DefaultRouteConfig()
	if validator != nil && rc.Validator == nil {
		rc.Validator = func(v interface{}) error { return validator.Validate(v) }
	}
	var sc *app.ServiceConfig
	if c, ok := any(svc).(auditedConfigurableService); ok {
		sc = cloneServiceConfig(c.GetConfig())
	} else {
		sc = cloneServiceConfig(app.DefaultServiceConfig())
	}
	return &AuditedApiBuilder[T]{routeConfig: rc, serviceConfig: sc, service: svc, validator: validator}
}

func (b *AuditedApiBuilder[T]) Route(config func(*RouteConfig)) IAuditedApiBuilder[T] {
	config(b.routeConfig)
	return b
}
func (b *AuditedApiBuilder[T]) Service(config func(*app.ServiceConfig)) IAuditedApiBuilder[T] {
	if config != nil {
		if b.serviceConfig == nil {
			b.serviceConfig = cloneServiceConfig(app.DefaultServiceConfig())
		}
		config(b.serviceConfig)
	}
	return b
}
func (b *AuditedApiBuilder[T]) Middleware(m ...core.Middleware) IAuditedApiBuilder[T] {
	b.middlewares = append(b.middlewares, m...)
	return b
}

func (b *AuditedApiBuilder[T]) Build(group core.IRouteGroup) error {
	if b.service == nil {
		return fmt.Errorf("service cannot be nil")
	}
	if b.serviceConfig != nil {
		if c, ok := any(b.service).(auditedConfigurableService); ok {
			c.UpdateConfig(cloneServiceConfig(b.serviceConfig))
		}
	}
	if b.validator != nil {
		if v, ok := any(b.service).(auditedValidatorAware); ok {
			v.SetValidator(b.validator)
		}
	}

	ar := NewAuditedRouteBuilder[T](b.service).WithConfig(b.routeConfig)
	if len(b.middlewares) > 0 {
		ar.Use(b.middlewares...)
	}
	return ar.Register(group)
}
