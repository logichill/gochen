package validate

import (
	"gochen/errors"
)

// IValidator 定义应用层可注入的策略校验器。
//
// 它用于需要替换、组合或依赖应用配置的校验规则；实体自身不变量仍由
// domain.IValidatable.Validate 表达，HTTP 请求体的协议级校验由 api/rest 单独配置。
type IValidator interface {
	Validate(value any) error
}

// Noop 默认验证器，实现为空操作。
type Noop struct{}

// Validate 校验输入。
func (Noop) Validate(value any) error {
	return nil
}

// NewError 创建一条带 `Validation` 错误码的校验错误。
func NewError(message string) error {
	return errors.NewCode(errors.Validation, message)
}
