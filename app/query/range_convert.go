package query

import (
	"fmt"
	"reflect"

	"gochen/errors"
)

func convertRange[T any](input queryValueRange) (Range[T], bool, error) {
	if input.Lower == nil && input.Upper == nil {
		return Range[T]{}, false, nil
	}
	var out Range[T]
	if input.Lower != nil {
		value, err := convertQueryValue[T](input.Lower.Value)
		if err != nil {
			return Range[T]{}, false, err
		}
		out.Lower = &Bound[T]{Value: value, Inclusive: input.Lower.Inclusive}
	}
	if input.Upper != nil {
		value, err := convertQueryValue[T](input.Upper.Value)
		if err != nil {
			return Range[T]{}, false, err
		}
		out.Upper = &Bound[T]{Value: value, Inclusive: input.Upper.Inclusive}
	}
	return out, true, nil
}

func convertQueryValue[T any](value QueryValue) (T, error) {
	var zero T
	targetType := reflect.TypeOf((*T)(nil)).Elem()
	if targetType == nil || targetType.Kind() == reflect.Interface {
		return zero, errors.NewCode(errors.InvalidInput, "query range does not support uninstantiated interface target type")
	}
	converted, err := convertQueryValueToType(targetType, value)
	if err != nil {
		return zero, err
	}
	return converted.Interface().(T), nil
}

func convertQueryValueToType(targetType reflect.Type, value QueryValue) (reflect.Value, error) {
	result := reflect.New(targetType).Elem()
	switch {
	case targetType == timeType:
		if value.Type != FieldTypeTime {
			return reflect.Value{}, incompatibleRangeValueError(targetType, value.Type)
		}
		result.Set(reflect.ValueOf(value.Time))
		return result, nil
	case targetType.Kind() == reflect.String:
		if value.Type != FieldTypeString && value.Type != FieldTypeEnum {
			return reflect.Value{}, incompatibleRangeValueError(targetType, value.Type)
		}
		result.SetString(value.String)
		return result, nil
	case isSignedIntKind(targetType.Kind()):
		if value.Type != FieldTypeInt {
			return reflect.Value{}, incompatibleRangeValueError(targetType, value.Type)
		}
		if err := ValidateSignedIntRange(targetType, value.Int); err != nil {
			return reflect.Value{}, err
		}
		result.SetInt(value.Int)
		return result, nil
	case isUnsignedIntKind(targetType.Kind()):
		if value.Type != FieldTypeInt {
			return reflect.Value{}, incompatibleRangeValueError(targetType, value.Type)
		}
		if err := ValidateUnsignedIntRange(targetType, value.Int); err != nil {
			return reflect.Value{}, err
		}
		result.SetUint(uint64(value.Int))
		return result, nil
	case isFloatKind(targetType.Kind()):
		if value.Type != FieldTypeFloat && value.Type != FieldTypeInt {
			return reflect.Value{}, incompatibleRangeValueError(targetType, value.Type)
		}
		val := value.Float
		if value.Type == FieldTypeInt {
			val = float64(value.Int)
		}
		if err := ValidateFloatRange(targetType, val); err != nil {
			return reflect.Value{}, err
		}
		result.SetFloat(val)
		return result, nil
	default:
		return reflect.Value{}, errors.NewCode(errors.InvalidInput, fmt.Sprintf("unsupported query range target: %s", targetType.String()))
	}
}

func incompatibleRangeValueError(targetType reflect.Type, valueType FieldType) error {
	message := "query range: incompatible query value type"
	if valueType == "" {
		message = "query range: query value type is required"
	}
	return errors.NewCode(errors.InvalidInput, message).
		WithContext("target", targetType.String()).
		WithContext("value_type", string(valueType))
}

func missingRangeValueTypeError(leftType, rightType FieldType) error {
	return errors.NewCode(errors.InvalidInput, "query range: query value type is required").
		WithContext("left_type", string(leftType)).
		WithContext("right_type", string(rightType))
}

func isSignedIntKind(kind reflect.Kind) bool {
	switch kind {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return true
	default:
		return false
	}
}

func isUnsignedIntKind(kind reflect.Kind) bool {
	switch kind {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return true
	default:
		return false
	}
}

func isFloatKind(kind reflect.Kind) bool {
	return kind == reflect.Float32 || kind == reflect.Float64
}
