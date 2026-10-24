package query

import (
	"math"
	"reflect"

	"gochen/errors"
)

// ValidateSignedIntRange 检查有符号整数是否可安全写入目标类型。
//
// targetType 必须是非 nil 的有符号整数类型；非法输入返回错误而不是 panic
// （本函数是导出的公共校验入口，调用方可能直接传入从反射/协议解析出的类型）。
func ValidateSignedIntRange(targetType reflect.Type, value int64) error {
	if err := requireIntegerType(targetType, true); err != nil {
		return err
	}
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
	if err := requireIntegerType(targetType, false); err != nil {
		return err
	}
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

// ValidateFloatRange 检查浮点数是否可安全写入目标类型，并拦截 NaN 与 Inf。
func ValidateFloatRange(targetType reflect.Type, value float64) error {
	if targetType == nil {
		return errors.NewCode(errors.InvalidInput, "target type is nil")
	}
	if targetType.Kind() != reflect.Float32 && targetType.Kind() != reflect.Float64 {
		return errors.NewCode(errors.InvalidInput, "target type is not a float type").
			WithContext("target_type", targetType.String())
	}
	if math.IsNaN(value) {
		return errors.NewCode(errors.InvalidInput, "float value cannot be NaN").
			WithContext("target_type", targetType.String())
	}
	if math.IsInf(value, 0) {
		return errors.NewCode(errors.InvalidInput, "float value cannot be infinite").
			WithContext("target_type", targetType.String())
	}
	if targetType.Kind() == reflect.Float32 {
		if math.Abs(value) > math.MaxFloat32 {
			return errors.NewCode(errors.InvalidInput, "float32 value out of range").
				WithContext("target_type", targetType.String()).
				WithContext("value", value)
		}
	}
	return nil
}

// requireIntegerType 校验公共范围校验函数的目标类型：非 nil 且为整数种类，
// 且符号性符合预期，避免直接调用 reflect.Type.Bits() 导致 panic。
func requireIntegerType(targetType reflect.Type, signed bool) error {
	if targetType == nil {
		return errors.NewCode(errors.InvalidInput, "target type is nil")
	}
	kind := targetType.Kind()
	valid := false
	switch kind {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		valid = signed
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		valid = !signed
	}
	if !valid {
		return errors.NewCode(errors.InvalidInput, "target type is not a valid integer type").
			WithContext("target_type", targetType.String())
	}
	return nil
}
