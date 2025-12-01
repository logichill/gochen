package validation

import (
	"fmt"
	"regexp"
	"strings"

	"gochen/errors"
)

var (
	emailRegex    = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
	usernameRegex = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)
)

// IValidator 定义通用验证器接口
type IValidator interface {
	Validate(value any) error
}

// NoopValidator 默认验证器，实现为空操作
type NoopValidator struct{}

// Validate 实现 IValidator 接口
func (NoopValidator) Validate(value any) error {
	return nil
}

// NewValidationError 创建验证错误
func NewValidationError(message string) error {
	return errors.NewValidationError(message)
}

// ValidateStringLength 验证字符串长度
func ValidateStringLength(value, fieldName string, min, max int) error {
	length := len(value)
	if length < min {
		return errors.NewError(errors.ErrCodeValidation,
			fmt.Sprintf("%s length must be at least %d characters (current %d)", fieldName, min, length))
	}
	if max > 0 && length > max {
		return errors.NewError(errors.ErrCodeValidation,
			fmt.Sprintf("%s length must be at most %d characters (current %d)", fieldName, max, length))
	}
	return nil
}

// ValidateRequired 验证必填字段
func ValidateRequired(value, fieldName string) error {
	if strings.TrimSpace(value) == "" {
		return errors.NewError(errors.ErrCodeValidation,
			fmt.Sprintf("%s cannot be empty", fieldName))
	}
	return nil
}

// ValidateIntRange 验证整数范围
func ValidateIntRange(value int, fieldName string, min, max int) error {
	if value < min {
		return errors.NewError(errors.ErrCodeValidation,
			fmt.Sprintf("%s cannot be less than %d (current %d)", fieldName, min, value))
	}
	if value > max {
		return errors.NewError(errors.ErrCodeValidation,
			fmt.Sprintf("%s cannot be greater than %d (current %d)", fieldName, max, value))
	}
	return nil
}

// ValidatePositive 验证正数
func ValidatePositive(value int, fieldName string) error {
	if value <= 0 {
		return errors.NewError(errors.ErrCodeValidation,
			fmt.Sprintf("%s must be positive (current %d)", fieldName, value))
	}
	return nil
}

// ValidateEmail 验证邮箱格式
func ValidateEmail(email string) error {
	if email == "" {
		return errors.NewError(errors.ErrCodeValidation, "email cannot be empty")
	}

	if !emailRegex.MatchString(email) {
		return errors.NewError(errors.ErrCodeValidation, "invalid email format")
	}
	return nil
}

// ValidateUsername 验证用户名
func ValidateUsername(username string) error {
	if err := ValidateRequired(username, "username"); err != nil {
		return err
	}

	if err := ValidateStringLength(username, "username", 3, 50); err != nil {
		return err
	}

	if !usernameRegex.MatchString(username) {
		return errors.NewError(errors.ErrCodeValidation,
			"username can only contain letters, numbers and underscores")
	}
	return nil
}

// ValidatePassword 验证密码强度
func ValidatePassword(password string) error {
	if err := ValidateRequired(password, "password"); err != nil {
		return err
	}

	if err := ValidateStringLength(password, "password", 6, 100); err != nil {
		return err
	}

	return nil
}

// ValidateEnum 验证枚举值
func ValidateEnum(value, fieldName string, validValues []string) error {
	for _, valid := range validValues {
		if value == valid {
			return nil
		}
	}
	return errors.NewError(errors.ErrCodeValidation,
		fmt.Sprintf("invalid value for %s, must be one of: %v", fieldName, validValues))
}

// ValidatePageParams 验证分页参数
func ValidatePageParams(page, pageSize int) error {
	if page <= 0 {
		return errors.NewError(errors.ErrCodeValidation, "page number must be greater than 0")
	}
	if pageSize <= 0 {
		return errors.NewError(errors.ErrCodeValidation, "page size must be greater than 0")
	}
	if pageSize > 100 {
		return errors.NewError(errors.ErrCodeValidation, "page size must not exceed 100")
	}
	return nil
}

// ValidateID 验证ID有效性
func ValidateID(id int64, fieldName string) error {
	if id <= 0 {
		return errors.NewError(errors.ErrCodeValidation,
			fmt.Sprintf("%s must be a positive integer", fieldName))
	}
	return nil
}
