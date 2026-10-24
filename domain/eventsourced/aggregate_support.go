package eventsourced

import (
	"gochen/errors"
)

// IEventSourcedModule 表示声明了 aggregate metadata 注册清单的模块。
type IEventSourcedModule interface {
	Name() string
	MetadataRegistrations() []MetadataRegistration
}

// Aggregate 创建一条 aggregate registration 声明。
//
// sample 允许为指向聚合结构体的 typed nil：仅用于反射获取类型，不会被解引用；
// 未携带具体类型的 nil interface 会被拒绝并记录在 registration.Error 中。
func Aggregate(sample any, aggregateType string) MetadataRegistration {
	var err error
	if sample == nil {
		err = errors.NewCode(errors.InvalidInput, "aggregate sample cannot be nil")
	}
	return MetadataRegistration{
		Sample:        sample,
		AggregateType: aggregateType,
		Error:         err,
	}
}

// AggregateFromTag 创建一条 aggregate registration 声明（aggregateType 从 struct tag 自动提取）。
func AggregateFromTag(sample any) MetadataRegistration {
	aggregateType, err := ResolveAggregateType(sample)
	return MetadataRegistration{
		Sample:        sample,
		AggregateType: aggregateType,
		Error:         err,
	}
}

// EventSourcedSupport 为模块提供复用的 aggregate registration 存储与访问能力。
type EventSourcedSupport struct {
	registrations []MetadataRegistration
}

// NewEventSourcedSupport 创建一个可嵌入模块的 aggregate registration support。
func NewEventSourcedSupport(registrations ...MetadataRegistration) EventSourcedSupport {
	return EventSourcedSupport{
		registrations: append([]MetadataRegistration(nil), registrations...),
	}
}

// MetadataRegistrations 返回模块声明的 aggregate metadata 注册清单。
func (s EventSourcedSupport) MetadataRegistrations() []MetadataRegistration {
	return append([]MetadataRegistration(nil), s.registrations...)
}

// RegisterModuleAggregates 在应用装配阶段统一注册并校验模块声明的 aggregate metadata。
func RegisterModuleAggregates(registry *MetadataRegistry, modules ...IEventSourcedModule) error {
	for _, module := range modules {
		if module == nil {
			continue
		}
		if err := registry.RegisterSet(module.MetadataRegistrations()...); err != nil {
			var appErr *errors.AppError
			if errors.As(err, &appErr) && appErr != nil {
				return appErr.Wrap("register aggregate metadata").
					WithContext("module", module.Name())
			}
			return errors.Wrap(err, errors.Internal, "register aggregate metadata").
				WithContext("module", module.Name())
		}
	}
	return nil
}
