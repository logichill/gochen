package validate

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"gochen/errors"
)

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
