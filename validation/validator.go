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
			fmt.Sprintf("%s长度不能少于%d个字符（当前%d）", fieldName, min, length))
	}
	if max > 0 && length > max {
		return errors.NewError(errors.ErrCodeValidation,
			fmt.Sprintf("%s长度不能超过%d个字符（当前%d）", fieldName, max, length))
	}
	return nil
}

// ValidateRequired 验证必填字段
func ValidateRequired(value, fieldName string) error {
	if strings.TrimSpace(value) == "" {
		return errors.NewError(errors.ErrCodeValidation,
			fmt.Sprintf("%s不能为空", fieldName))
	}
	return nil
}

// ValidateIntRange 验证整数范围
func ValidateIntRange(value int, fieldName string, min, max int) error {
	if value < min {
		return errors.NewError(errors.ErrCodeValidation,
			fmt.Sprintf("%s不能小于%d（当前%d）", fieldName, min, value))
	}
	if value > max {
		return errors.NewError(errors.ErrCodeValidation,
			fmt.Sprintf("%s不能大于%d（当前%d）", fieldName, max, value))
	}
	return nil
}

// ValidatePositive 验证正数
func ValidatePositive(value int, fieldName string) error {
	if value <= 0 {
		return errors.NewError(errors.ErrCodeValidation,
			fmt.Sprintf("%s必须为正数（当前%d）", fieldName, value))
	}
	return nil
}

// ValidateEmail 验证邮箱格式
func ValidateEmail(email string) error {
	if email == "" {
		return errors.NewError(errors.ErrCodeValidation, "邮箱不能为空")
	}

	if !emailRegex.MatchString(email) {
		return errors.NewError(errors.ErrCodeValidation, "邮箱格式不正确")
	}
	return nil
}

// ValidateUsername 验证用户名
func ValidateUsername(username string) error {
	if err := ValidateRequired(username, "用户名"); err != nil {
		return err
	}

	if err := ValidateStringLength(username, "用户名", 3, 50); err != nil {
		return err
	}

	if !usernameRegex.MatchString(username) {
		return errors.NewError(errors.ErrCodeValidation,
			"用户名只能包含字母、数字和下划线")
	}
	return nil
}

// ValidatePassword 验证密码强度
func ValidatePassword(password string) error {
	if err := ValidateRequired(password, "密码"); err != nil {
		return err
	}

	if err := ValidateStringLength(password, "密码", 6, 100); err != nil {
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
		fmt.Sprintf("%s的值无效，必须是以下之一: %v", fieldName, validValues))
}

// ValidatePageParams 验证分页参数
func ValidatePageParams(page, pageSize int) error {
	if page <= 0 {
		return errors.NewError(errors.ErrCodeValidation, "页码必须大于0")
	}
	if pageSize <= 0 {
		return errors.NewError(errors.ErrCodeValidation, "每页大小必须大于0")
	}
	if pageSize > 100 {
		return errors.NewError(errors.ErrCodeValidation, "每页大小不能超过100")
	}
	return nil
}

// ValidateID 验证ID有效性
func ValidateID(id int64, fieldName string) error {
	if id <= 0 {
		return errors.NewError(errors.ErrCodeValidation,
			fmt.Sprintf("%s必须为正整数", fieldName))
	}
	return nil
}
