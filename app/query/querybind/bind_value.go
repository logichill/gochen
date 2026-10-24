package querybind

import (
	"fmt"
	"reflect"
	"time"

	"gochen/app/query"
	"gochen/errors"
)

var timeType = reflect.TypeOf(time.Time{})

func bindDefault(dst reflect.Value, exprs []query.QueryExpr) error {
	if len(exprs) == 0 {
		return nil
	}

	if dst.Kind() == reflect.Ptr {
		if err := validateBindPointerType(dst.Type(), map[reflect.Type]bool{}); err != nil {
			return err
		}
		if dst.IsNil() {
			// 先在临时值上递归绑定，成功后才写回目标：避免绑定失败时
			// 把零值指针污染给调用方（P2-030）。
			allocated := reflect.New(dst.Type().Elem())
			if err := bindDefault(allocated.Elem(), exprs); err != nil {
				return err
			}
			dst.Set(allocated)
			return nil
		}
		return bindDefault(dst.Elem(), exprs)
	}

	if dst.Kind() == reflect.Slice {
		return bindSlice(dst, exprs)
	}

	if len(exprs) != 1 {
		return errors.NewCode(errors.InvalidInput, "default binding expects a single query expression")
	}
	if exprs[0].Op != query.FilterOpEq {
		return errors.NewCode(errors.InvalidInput, "default binding only supports eq operator").
			WithContext("operator", string(exprs[0].Op))
	}

	return assignScalar(dst, exprs[0].Value)
}

func validateBindPointerType(t reflect.Type, path map[reflect.Type]bool) error {
	for t != nil && t.Kind() == reflect.Pointer {
		if path[t] {
			return errors.NewCode(errors.InvalidInput, "querybind: recursive pointer type").WithContext("type", t.String())
		}
		path[t] = true
		t = t.Elem()
	}
	return nil
}

func bindSlice(dst reflect.Value, exprs []query.QueryExpr) error {
	if len(exprs) != 1 {
		return errors.NewCode(errors.InvalidInput, "slice binding expects a single query expression")
	}
	expr := exprs[0]
	if expr.Op != query.FilterOpEq && expr.Op != query.FilterOpIn {
		return errors.NewCode(errors.InvalidInput, "slice binding only supports eq/in operator").
			WithContext("operator", string(expr.Op))
	}

	values := expr.Values
	if expr.Op == query.FilterOpEq {
		values = []query.QueryValue{expr.Value}
	}

	result := reflect.MakeSlice(dst.Type(), 0, len(values))
	for _, value := range values {
		elem := reflect.New(dst.Type().Elem()).Elem()
		// 指针切片（[]*T）：分配指向元素并解引用后再赋值，
		// 支持业务 DTO 用指针区分零值与显式传入。
		if elem.Kind() == reflect.Ptr {
			v := reflect.New(elem.Type().Elem()).Elem()
			if err := assignScalar(v, value); err != nil {
				return err
			}
			allocated := reflect.New(elem.Type().Elem())
			allocated.Elem().Set(v)
			elem.Set(allocated)
		} else if err := assignScalar(elem, value); err != nil {
			return err
		}
		result = reflect.Append(result, elem)
	}
	dst.Set(result)
	return nil
}

func assignScalar(dst reflect.Value, value query.QueryValue) error {
	targetType := dst.Type()
	switch {
	case targetType == timeType:
		if value.Type != query.FieldTypeTime {
			return incompatibleScalarValueError(targetType, value.Type)
		}
		dst.Set(reflect.ValueOf(value.Time))
		return nil
	case targetType.Kind() == reflect.String:
		if value.Type != query.FieldTypeString && value.Type != query.FieldTypeEnum {
			return incompatibleScalarValueError(targetType, value.Type)
		}
		dst.SetString(value.String)
		return nil
	case isSignedInt(targetType.Kind()):
		if value.Type != query.FieldTypeInt {
			return incompatibleScalarValueError(targetType, value.Type)
		}
		if err := query.ValidateSignedIntRange(targetType, value.Int); err != nil {
			return err
		}
		dst.SetInt(value.Int)
		return nil
	case isUnsignedInt(targetType.Kind()):
		if value.Type != query.FieldTypeInt {
			return incompatibleScalarValueError(targetType, value.Type)
		}
		if err := query.ValidateUnsignedIntRange(targetType, value.Int); err != nil {
			return err
		}
		dst.SetUint(uint64(value.Int))
		return nil
	case isFloat(targetType.Kind()):
		if value.Type != query.FieldTypeFloat && value.Type != query.FieldTypeInt {
			return incompatibleScalarValueError(targetType, value.Type)
		}
		val := value.Float
		if value.Type == query.FieldTypeInt {
			val = float64(value.Int)
		}
		if err := query.ValidateFloatRange(targetType, val); err != nil {
			return err
		}
		dst.SetFloat(val)
		return nil
	case targetType.Kind() == reflect.Bool:
		if value.Type != query.FieldTypeBool {
			return incompatibleScalarValueError(targetType, value.Type)
		}
		dst.SetBool(value.Bool)
		return nil
	default:
		return errors.NewCode(errors.InvalidInput, fmt.Sprintf("unsupported default bind target: %s", targetType.String()))
	}
}

func incompatibleScalarValueError(targetType reflect.Type, valueType query.FieldType) error {
	message := "querybind: incompatible query value type"
	if valueType == "" {
		message = "querybind: query value type is required"
	}
	return errors.NewCode(errors.InvalidInput, message).
		WithContext("target", targetType.String()).
		WithContext("value_type", string(valueType))
}

func isSignedInt(kind reflect.Kind) bool {
	switch kind {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return true
	default:
		return false
	}
}

func isUnsignedInt(kind reflect.Kind) bool {
	switch kind {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return true
	default:
		return false
	}
}

func isFloat(kind reflect.Kind) bool {
	return kind == reflect.Float32 || kind == reflect.Float64
}
