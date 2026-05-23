package config

import (
	"fmt"
	"math"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

// ValidatorFunc 验证函数类型。
type ValidatorFunc func(value any) error

// ConfigSchema 定义配置结构定义。
type ConfigSchema struct {
	// Required 必填配置键列表
	Required []string

	// Defaults 默认值映射（键 → 默认值）
	Defaults map[string]any

	// Validators 验证器映射（键 → 验证函数）
	Validators map[string]ValidatorFunc
}

// SchemaValidationResult 配置验证结果。
type SchemaValidationResult struct {
	// Valid 是否全部验证通过
	Valid bool

	// Errors 验证错误列表
	Errors []ValidationError

	// Warnings 验证警告列表（非致命问题）
	Warnings []ValidationError
}

// LoadConfig 加载并规范化配置。
func LoadConfig[T any](provider IConfigProvider, schema ConfigSchema) (*T, []ValidationError) {
	applyDefaultsToProvider(provider, schema.Defaults)

	errors := ValidateWithSchema(provider, schema)

	var cfg T
	if err := provider.Bind("", &cfg); err != nil {
		errors = append(errors, ValidationError{
			Key:     "",
			Message: fmt.Sprintf("failed to bind configuration: %v", err),
		})
	}

	return &cfg, errors
}

// LoadWithSchema 加载带结构定义。
func LoadWithSchema[T any](provider IConfigProvider, schema ConfigSchema) (*T, []ValidationError) {
	return LoadConfig[T](provider, schema)
}

// ValidateWithSchema 校验带结构定义。
func ValidateWithSchema(provider IConfigProvider, schema ConfigSchema) []ValidationError {
	var errors []ValidationError

	// 1. 检查必填项
	for _, key := range schema.Required {
		if !provider.Has(key) {
			// 检查是否有默认值
			if _, hasDefault := schema.Defaults[key]; !hasDefault {
				errors = append(errors, ValidationError{
					Key:     key,
					Message: "required configuration missing",
				})
			}
		}
	}

	// 2. 执行验证器
	for key, validator := range schema.Validators {
		value := provider.Get(key)
		if value == nil {
			// 使用默认值
			if def, ok := schema.Defaults[key]; ok {
				value = def
			}
		}

		if value != nil {
			if err := validator(value); err != nil {
				errors = append(errors, ValidationError{
					Key:     key,
					Message: err.Error(),
				})
			}
		}
	}

	return errors
}

// ApplyDefaults 应用Defaults。
func ApplyDefaults(values map[string]any, schema ConfigSchema) map[string]any {
	result := make(map[string]any, len(values)+len(schema.Defaults))

	// 先复制默认值
	for k, v := range schema.Defaults {
		result[k] = v
	}

	// 用实际值覆盖
	for k, v := range values {
		result[k] = v
	}

	return result
}

// applyDefaultsToProvider 应用Defaults到提供者。
func applyDefaultsToProvider(provider IConfigProvider, defaults map[string]any) {
	if provider == nil || len(defaults) == 0 {
		return
	}

	p, ok := provider.(*Provider)
	if !ok || p == nil {
		return
	}

	for k, v := range defaults {
		existing, exists := p.settings[k]
		if !exists || existing == nil {
			p.settings[k] = v
		}
	}
}

// RangeValidator 创建数值范围验证器。
func RangeValidator(min, max int) ValidatorFunc {
	return func(value any) error {
		v, ok := toInt(value)
		if !ok {
			return fmt.Errorf("expected integer, got %T", value)
		}
		if v < min || v > max {
			return fmt.Errorf("value %d out of range [%d, %d]", v, min, max)
		}
		return nil
	}
}

// MinValidator 创建最小值验证器。
func MinValidator(min int) ValidatorFunc {
	return func(value any) error {
		v, ok := toInt(value)
		if !ok {
			return fmt.Errorf("expected integer, got %T", value)
		}
		if v < min {
			return fmt.Errorf("value %d is less than minimum %d", v, min)
		}
		return nil
	}
}

// MaxValidator 创建最大值验证器。
func MaxValidator(max int) ValidatorFunc {
	return func(value any) error {
		v, ok := toInt(value)
		if !ok {
			return fmt.Errorf("expected integer, got %T", value)
		}
		if v > max {
			return fmt.Errorf("value %d is greater than maximum %d", v, max)
		}
		return nil
	}
}

// NotEmptyValidator 创建非空字符串验证器。
func NotEmptyValidator() ValidatorFunc {
	return func(value any) error {
		s, ok := value.(string)
		if !ok {
			return fmt.Errorf("expected string, got %T", value)
		}
		if s == "" {
			return fmt.Errorf("value cannot be empty")
		}
		return nil
	}
}

// OneOfValidator 创建枚举验证器。
func OneOfValidator(allowed ...string) ValidatorFunc {
	allowedSet := make(map[string]bool, len(allowed))
	for _, a := range allowed {
		allowedSet[a] = true
	}

	return func(value any) error {
		s, ok := value.(string)
		if !ok {
			return fmt.Errorf("expected string, got %T", value)
		}
		if !allowedSet[s] {
			return fmt.Errorf("value %q not in allowed values %v", s, allowed)
		}
		return nil
	}
}

// PatternValidator 创建正则表达式验证器。
func PatternValidator(pattern string) ValidatorFunc {
	return func(value any) error {
		s, ok := value.(string)
		if !ok {
			return fmt.Errorf("expected string, got %T", value)
		}
		matched, err := matchPattern(pattern, s)
		if err != nil {
			return fmt.Errorf("invalid pattern %q: %w", pattern, err)
		}
		if !matched {
			return fmt.Errorf("value %q does not match pattern %s", s, pattern)
		}
		return nil
	}
}

// URLValidator 创建 URL 格式验证器。
func URLValidator() ValidatorFunc {
	return PatternValidator(`^https?://[^\s/$.?#].[^\s]*$`)
}

// EmailValidator 创建邮箱格式验证器。
func EmailValidator() ValidatorFunc {
	return PatternValidator(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
}

// toInt 尝试将值转换为 int。
func toInt(value any) (int, bool) {
	if value == nil {
		return 0, false
	}

	// NOTE: This function is used for validation only. We avoid "best-effort truncation"
	// (e.g. float64(42.5) -> 42) because it silently changes user input.
	// For interface-driven numbers (json.Number), Kind() is string, so we parse it too.
	maxInt := int64(^uint(0) >> 1)
	minInt := -maxInt - 1

	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		i := v.Int()
		if i < minInt || i > maxInt {
			return 0, false
		}
		return int(i), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		u := v.Uint()
		if u > uint64(maxInt) {
			return 0, false
		}
		return int(u), true
	case reflect.Float32, reflect.Float64:
		f := v.Float()
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, false
		}
		if f != math.Trunc(f) {
			return 0, false
		}
		if f < float64(minInt) || f > float64(maxInt) {
			return 0, false
		}
		return int(f), true
	case reflect.String:
		s := strings.TrimSpace(v.String())
		if s == "" {
			return 0, false
		}
		// bitSize=0 means int.
		i, err := strconv.ParseInt(s, 10, 0)
		if err != nil {
			return 0, false
		}
		return int(i), true
	default:
		return 0, false
	}
}

// matchPattern 使用正则表达式匹配。
func matchPattern(pattern, s string) (bool, error) {
	return regexp.MatchString(pattern, s)
}
