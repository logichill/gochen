package validate

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"gochen/errors"
)

var (
	emailRegex    = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
	usernameRegex = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)
)

// IValidator 定义通用验证器接口。
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

// StringLength 校验字符串长度是否落在给定区间内。
func StringLength(value, fieldName string, min, max int) error {
	// 按 Unicode 字符数而非字节数计算长度，避免多字节字符（如中文/emoji）导致结果不符合直觉。
	length := utf8.RuneCountInString(value)
	if length < min {
		return errors.NewCode(errors.Validation,
			fmt.Sprintf("%s length must be at least %d characters (current %d)", fieldName, min, length))
	}
	if max > 0 && length > max {
		return errors.NewCode(errors.Validation,
			fmt.Sprintf("%s length must be at most %d characters (current %d)", fieldName, max, length))
	}
	return nil
}

// Required 校验字符串字段非空。
func Required(value, fieldName string) error {
	if strings.TrimSpace(value) == "" {
		return errors.NewCode(errors.Validation,
			fmt.Sprintf("%s cannot be empty", fieldName))
	}
	return nil
}

// IntRange 校验整数是否位于给定闭区间内。
func IntRange(value int, fieldName string, min, max int) error {
	if value < min {
		return errors.NewCode(errors.Validation,
			fmt.Sprintf("%s cannot be less than %d (current %d)", fieldName, min, value))
	}
	if value > max {
		return errors.NewCode(errors.Validation,
			fmt.Sprintf("%s cannot be greater than %d (current %d)", fieldName, max, value))
	}
	return nil
}

// Positive 校验整数是否为正数。
func Positive(value int, fieldName string) error {
	if value <= 0 {
		return errors.NewCode(errors.Validation,
			fmt.Sprintf("%s must be positive (current %d)", fieldName, value))
	}
	return nil
}

// Email 校验邮箱地址格式。
func Email(email string) error {
	if email == "" {
		return errors.NewCode(errors.Validation, "email cannot be empty")
	}

	if !emailRegex.MatchString(email) {
		return errors.NewCode(errors.Validation, "invalid email format")
	}
	return nil
}

// Username 校验用户名是否满足长度与字符集约束。
func Username(username string) error {
	if err := Required(username, "username"); err != nil {
		return err
	}

	if err := StringLength(username, "username", 3, 50); err != nil {
		return err
	}

	if !usernameRegex.MatchString(username) {
		return errors.NewCode(errors.Validation,
			"username can only contain letters, numbers and underscores")
	}
	return nil
}

// Password 校验密码是否满足最基本的长度要求。
func Password(password string) error {
	if err := Required(password, "password"); err != nil {
		return err
	}

	if err := StringLength(password, "password", 6, 100); err != nil {
		return err
	}

	return nil
}

// Enum 校验字符串值是否命中允许的枚举集合。
func Enum(value, fieldName string, validValues []string) error {
	for _, valid := range validValues {
		if value == valid {
			return nil
		}
	}
	return errors.NewCode(errors.Validation,
		fmt.Sprintf("invalid value for %s, must be one of: %v", fieldName, validValues))
}

// PageParams 校验分页参数是否合法。
func PageParams(page, pageSize int) error {
	if page <= 0 {
		return errors.NewCode(errors.Validation, "page number must be greater than 0")
	}
	if pageSize <= 0 {
		return errors.NewCode(errors.Validation, "page size must be greater than 0")
	}
	if pageSize > 100 {
		return errors.NewCode(errors.Validation, "page size must not exceed 100")
	}
	return nil
}

// ID 校验整型标识是否为正数。
func ID(id int64, fieldName string) error {
	if id <= 0 {
		return errors.NewCode(errors.Validation,
			fmt.Sprintf("%s must be a positive integer", fieldName))
	}
	return nil
}
