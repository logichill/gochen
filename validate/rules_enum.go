package validate

import (
	"fmt"

	"gochen/errors"
)

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
