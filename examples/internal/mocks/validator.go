// Package mocks 提供示例中使用的模拟组件
package mocks

import (
	"gochen/domain"
	"gochen/errors"
	"gochen/validation"
)

// GenericValidator 通用验证器
type GenericValidator[T domain.IEntity[int64]] struct {
	validateFunc func(T) error
}

// NewGenericValidator 创建通用验证器
func NewGenericValidator[T domain.IEntity[int64]](validateFunc func(T) error) *GenericValidator[T] {
	return &GenericValidator[T]{
		validateFunc: validateFunc,
	}
}

func (v *GenericValidator[T]) Validate(entity any) error {
	if typedEntity, ok := entity.(T); ok {
		return v.validateFunc(typedEntity)
	}
	return errors.NewValidationError("实体类型不匹配")
}

// SimpleEntityValidator 简单实体验证器
type SimpleEntityValidator struct {
	validateFunc func(any) error
}

// NewSimpleEntityValidator 创建简单实体验证器
func NewSimpleEntityValidator(validateFunc func(any) error) *SimpleEntityValidator {
	return &SimpleEntityValidator{
		validateFunc: validateFunc,
	}
}

func (v *SimpleEntityValidator) Validate(entity any) error {
	return v.validateFunc(entity)
}

// noopValidator 一个什么都不做的验证器，实现 validation.IValidator
type noopValidator struct{}

func (n *noopValidator) Validate(any) error { return nil }

// NewNoopValidator 返回一个总是通过的验证器，便于示例快速接线
func NewNoopValidator() validation.IValidator { return &noopValidator{} }
