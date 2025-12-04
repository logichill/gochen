package api

import (
	"fmt"

	application "gochen/app/application"
	"gochen/domain/audited"
	httpx "gochen/http"
	"gochen/validation"
)

// IAuditedApiBuilder 面向审计型业务的构建器接口
type IAuditedApiBuilder[T audited.IAuditedEntity[int64]] interface {
	Route(config func(*RouteConfig)) IAuditedApiBuilder[T]
	Service(config func(*application.ServiceConfig)) IAuditedApiBuilder[T]
	Middleware(middlewares ...httpx.Middleware) IAuditedApiBuilder[T]
	Build(group httpx.IRouteGroup) error
}

type auditedConfigurableService interface {
	UpdateConfig(*application.ServiceConfig)
	GetConfig() *application.ServiceConfig
}
type auditedValidatorAware interface{ SetValidator(validation.IValidator) }

// AuditedApiBuilder 构建器实现
type AuditedApiBuilder[T audited.IAuditedEntity[int64]] struct {
	routeConfig   *RouteConfig
	serviceConfig *application.ServiceConfig
	middlewares   []httpx.Middleware
	service       audited.IAuditedService[T, int64]
	validator     validation.IValidator
}

func NewAuditedApiBuilder[T audited.IAuditedEntity[int64]](svc audited.IAuditedService[T, int64], validator validation.IValidator) *AuditedApiBuilder[T] {
	rc := DefaultRouteConfig()
	if validator != nil && rc.Validator == nil {
		rc.Validator = func(v any) error { return validator.Validate(v) }
	}
	var sc *application.ServiceConfig
	if c, ok := any(svc).(auditedConfigurableService); ok {
		sc = cloneServiceConfig(c.GetConfig())
	} else {
		sc = cloneServiceConfig(application.DefaultServiceConfig())
	}
	return &AuditedApiBuilder[T]{routeConfig: rc, serviceConfig: sc, service: svc, validator: validator}
}

func (b *AuditedApiBuilder[T]) Route(config func(*RouteConfig)) IAuditedApiBuilder[T] {
	config(b.routeConfig)
	return b
}
func (b *AuditedApiBuilder[T]) Service(config func(*application.ServiceConfig)) IAuditedApiBuilder[T] {
	if config != nil {
		if b.serviceConfig == nil {
			b.serviceConfig = cloneServiceConfig(application.DefaultServiceConfig())
		}
		config(b.serviceConfig)
	}
	return b
}
func (b *AuditedApiBuilder[T]) Middleware(m ...httpx.Middleware) IAuditedApiBuilder[T] {
	b.middlewares = append(b.middlewares, m...)
	return b
}

func (b *AuditedApiBuilder[T]) Build(group httpx.IRouteGroup) error {
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
