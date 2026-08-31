package validate

import (
	"fmt"

	"gochen/errors"
)

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

// ID 校验整型标识是否为正数。
func ID(id int64, fieldName string) error {
	if id <= 0 {
		return errors.NewCode(errors.Validation,
			fmt.Sprintf("%s must be a positive integer", fieldName))
	}
	return nil
}
