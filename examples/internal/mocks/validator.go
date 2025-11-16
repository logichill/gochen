// Package mocks 提供示例中使用的模拟组件
package mocks

import (
	"gochen/domain/entity"
	"gochen/domain/service"
	validation "gochen/validation"
)

// GenericValidator 通用验证器
type GenericValidator[T entity.IEntity[int64]] struct {
	validateFunc func(T) error
}

// NewGenericValidator 创建通用验证器
func NewGenericValidator[T entity.IEntity[int64]](validateFunc func(T) error) *GenericValidator[T] {
	return &GenericValidator[T]{
		validateFunc: validateFunc,
	}
}

func (v *GenericValidator[T]) Validate(entity interface{}) error {
	if typedEntity, ok := entity.(T); ok {
		return v.validateFunc(typedEntity)
	}
	return service.NewValidationError("实体类型不匹配")
}

// SimpleEntityValidator 简单实体验证器
type SimpleEntityValidator struct {
	validateFunc func(interface{}) error
}

// NewSimpleEntityValidator 创建简单实体验证器
func NewSimpleEntityValidator(validateFunc func(interface{}) error) *SimpleEntityValidator {
	return &SimpleEntityValidator{
		validateFunc: validateFunc,
	}
}

func (v *SimpleEntityValidator) Validate(entity interface{}) error {
	return v.validateFunc(entity)
}

// noopValidator 一个什么都不做的验证器，实现 validation.IValidator
type noopValidator struct{}

func (n *noopValidator) Validate(interface{}) error { return nil }

// NewNoopValidator 返回一个总是通过的验证器，便于示例快速接线
func NewNoopValidator() validation.IValidator { return &noopValidator{} }
