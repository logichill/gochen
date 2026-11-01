package meta

import (
	"fmt"
	"reflect"
	"strconv"

	"gochen/errors"
)

// convertTo 实现 D5 的唯一一套转换规则：
//   - 目标 string：接受 string 与整数（十进制）；
//   - 目标整数：接受整数与 string（十进制解析，检查溢出）；
//   - 目标 bool：接受 bool 与可解析的 string；
//   - 目标指针：先转换元素类型，再取地址。
func convertTo(t reflect.Type, v any, field string) (reflect.Value, error) {
	if v == nil {
		return reflect.Value{}, conversionError(field, t, v, "value cannot be nil")
	}
	if t.Kind() == reflect.Ptr {
		elem, err := convertTo(t.Elem(), v, field)
		if err != nil {
			return reflect.Value{}, err
		}
		ptr := reflect.New(t.Elem())
		ptr.Elem().Set(elem)
		return ptr, nil
	}

	src := reflect.ValueOf(v)
	if src.Kind() == reflect.Ptr {
		if src.IsNil() {
			return reflect.Value{}, conversionError(field, t, v, "value cannot be nil")
		}
		src = src.Elem()
		v = src.Interface()
	}

	switch t.Kind() {
	case reflect.String:
		switch src.Kind() {
		case reflect.String:
			out := reflect.New(t).Elem()
			out.SetString(src.String())
			return out, nil
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			out := reflect.New(t).Elem()
			out.SetString(strconv.FormatInt(src.Int(), 10))
			return out, nil
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			out := reflect.New(t).Elem()
			out.SetString(strconv.FormatUint(src.Uint(), 10))
			return out, nil
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		out := reflect.New(t).Elem()
		switch src.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			n := src.Int()
			if out.OverflowInt(n) {
				return reflect.Value{}, conversionError(field, t, v, "integer overflow")
			}
			out.SetInt(n)
			return out, nil
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			u := src.Uint()
			if u > uint64(^uint64(0)>>1) {
				return reflect.Value{}, conversionError(field, t, v, "integer overflow")
			}
			n := int64(u)
			if out.OverflowInt(n) {
				return reflect.Value{}, conversionError(field, t, v, "integer overflow")
			}
			out.SetInt(n)
			return out, nil
		case reflect.String:
			n, err := strconv.ParseInt(src.String(), 10, t.Bits())
			if err != nil {
				return reflect.Value{}, conversionError(field, t, v, "invalid integer literal")
			}
			out.SetInt(n)
			return out, nil
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		out := reflect.New(t).Elem()
		switch src.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			n := src.Int()
			if n < 0 || out.OverflowUint(uint64(n)) {
				return reflect.Value{}, conversionError(field, t, v, "integer overflow")
			}
			out.SetUint(uint64(n))
			return out, nil
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			u := src.Uint()
			if out.OverflowUint(u) {
				return reflect.Value{}, conversionError(field, t, v, "integer overflow")
			}
			out.SetUint(u)
			return out, nil
		case reflect.String:
			u, err := strconv.ParseUint(src.String(), 10, t.Bits())
			if err != nil {
				return reflect.Value{}, conversionError(field, t, v, "invalid integer literal")
			}
			out.SetUint(u)
			return out, nil
		}
	case reflect.Bool:
		out := reflect.New(t).Elem()
		switch src.Kind() {
		case reflect.Bool:
			out.SetBool(src.Bool())
			return out, nil
		case reflect.String:
			b, err := strconv.ParseBool(src.String())
			if err != nil {
				return reflect.Value{}, conversionError(field, t, v, "invalid bool literal")
			}
			out.SetBool(b)
			return out, nil
		}
	case reflect.Interface:
		if t.NumMethod() == 0 {
			out := reflect.New(t).Elem()
			out.Set(src)
			return out, nil
		}
	}

	return reflect.Value{}, conversionError(field, t, v, "unsupported conversion")
}

func conversionError(field string, t reflect.Type, v any, reason string) error {
	return errors.NewCode(errors.InvalidInput, "field value conversion failed").
		WithContext("field", field).
		WithContext("target", t.String()).
		WithContext("value_type", fmt.Sprintf("%T", v)).
		WithContext("reason", reason)
}
