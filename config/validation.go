package config

import (
	"gochen/errors"
)

// Validator 配置验证器。
type Validator struct {
	required []string
	rules    []validationRule
}

type validationRule struct {
	key       string
	validator ValidatorFunc
}

// NewValidator 创建Validator。
func NewValidator() *Validator {
	return &Validator{
		required: make([]string, 0),
		rules:    make([]validationRule, 0),
	}
}

// Required 添加必填验证规则。
func (v *Validator) Required(key string) *Validator {
	v.required = append(v.required, key)
	return v
}

// Range 添加数值范围验证规则。
func (v *Validator) Range(key string, min, max int) *Validator {
	v.rules = append(v.rules, validationRule{key: key, validator: RangeValidator(min, max)})
	return v
}

// Min 添加最小值验证规则。
func (v *Validator) Min(key string, min int) *Validator {
	v.rules = append(v.rules, validationRule{key: key, validator: MinValidator(min)})
	return v
}

// Max 添加最大值验证规则。
func (v *Validator) Max(key string, max int) *Validator {
	v.rules = append(v.rules, validationRule{key: key, validator: MaxValidator(max)})
	return v
}

// NotEmpty 添加非空字符串验证规则。
func (v *Validator) NotEmpty(key string) *Validator {
	v.rules = append(v.rules, validationRule{key: key, validator: NotEmptyValidator()})
	return v
}

// Pattern 添加正则表达式验证规则。
func (v *Validator) Pattern(key string, pattern string) *Validator {
	v.rules = append(v.rules, validationRule{key: key, validator: PatternValidator(pattern)})
	return v
}

// OneOf 添加枚举验证规则。
func (v *Validator) OneOf(key string, allowed ...string) *Validator {
	v.rules = append(v.rules, validationRule{key: key, validator: OneOfValidator(allowed...)})
	return v
}

// Custom 添加自定义验证规则。
func (v *Validator) Custom(key string, fn func(value any) error) *Validator {
	v.rules = append(v.rules, validationRule{key: key, validator: fn})
	return v
}

// Validate 执行验证。
func (v *Validator) Validate(provider IConfigProvider) error {
	schema := ConfigSchema{
		Required:   append([]string(nil), v.required...),
		Defaults:   map[string]any{},
		Validators: make(map[string]ValidatorFunc, len(v.rules)),
	}
	for _, rule := range v.rules {
		schema.Validators[rule.key] = rule.validator
	}

	validationErrors := ValidateWithSchema(provider, schema)
	if len(validationErrors) == 0 {
		return nil
	}
	for i := range validationErrors {
		validationErrors[i].Message = normalizeValidationMessage(validationErrors[i].Message)
	}
	return ValidationErrors(validationErrors)
}

// normalizeValidationMessage 规范化Validation消息。
func normalizeValidationMessage(message string) string {
	if message == "" {
		return errors.NewCode(errors.Validation, "invalid configuration").Message()
	}
	return message
}

// 接口断言。
var _ IConfigValidator = (*Validator)(nil)
