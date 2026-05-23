// Package mocks 提供示例中使用的模拟组件
package mocks

import (
	"gochen/domain"
	"gochen/errors"
	"gochen/validate"
)

// GenericValidator 把类型化校验函数适配为通用验证器接口。
type GenericValidator[T domain.IEntity[int64]] struct {
	validateFunc func(T) error
}

// NewGenericValidator 创建一个基于类型断言的泛型验证器。
func NewGenericValidator[T domain.IEntity[int64]](validateFunc func(T) error) *GenericValidator[T] {
	return &GenericValidator[T]{
		validateFunc: validateFunc,
	}
}

// Validate 在输入类型匹配时调用底层类型化校验函数。
func (v *GenericValidator[T]) Validate(entity any) error {
	if typedEntity, ok := entity.(T); ok {
		return v.validateFunc(typedEntity)
	}
	return errors.NewCode(errors.Validation, "实体类型不匹配")
}

// SimpleEntityValidator 直接包装一个 `func(any) error` 形式的验证函数。
type SimpleEntityValidator struct {
	validateFunc func(any) error
}

// NewSimpleEntityValidator 创建一个简单的函数式验证器。
func NewSimpleEntityValidator(validateFunc func(any) error) *SimpleEntityValidator {
	return &SimpleEntityValidator{
		validateFunc: validateFunc,
	}
}

// Validate 直接委托到底层校验函数。
func (v *SimpleEntityValidator) Validate(entity any) error {
	return v.validateFunc(entity)
}

// noopValidator 是一个始终通过的验证器。
type noopValidator struct{}

// Validate 对任何输入都返回 nil。
func (n *noopValidator) Validate(any) error { return nil }

// NewNoopValidator 创建一个空实现验证器。
func NewNoopValidator() validate.IValidator { return &noopValidator{} }
