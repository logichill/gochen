package query

import (
	"math"
	"reflect"

	"gochen/errors"
)

// ValidateSignedIntRange 检查有符号整数是否可安全写入目标类型。
func ValidateSignedIntRange(targetType reflect.Type, value int64) error {
	bits := targetType.Bits()
	if bits <= 0 || bits >= 64 {
		return nil
	}
	min := -(int64(1) << (bits - 1))
	max := (int64(1) << (bits - 1)) - 1
	if value < min || value > max {
		return errors.NewCode(errors.InvalidInput, "signed integer value out of range").
			WithContext("target_type", targetType.String()).
			WithContext("value", value)
	}
	return nil
}

// ValidateUnsignedIntRange 检查无符号整数是否可安全写入目标类型。
func ValidateUnsignedIntRange(targetType reflect.Type, value int64) error {
	if value < 0 {
		return errors.NewCode(errors.InvalidInput, "unsigned integer value cannot be negative").
			WithContext("target_type", targetType.String()).
			WithContext("value", value)
	}
	bits := targetType.Bits()
	if bits <= 0 || bits >= 64 {
		return nil
	}
	max := uint64(math.MaxUint64 >> (64 - bits))
	if uint64(value) > max {
		return errors.NewCode(errors.InvalidInput, "unsigned integer value out of range").
			WithContext("target_type", targetType.String()).
			WithContext("value", value)
	}
	return nil
}
