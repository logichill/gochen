package meta

import (
	"reflect"
	"time"

	"gochen/errors"
)

type clonePointer struct {
	typ     reflect.Type
	pointer uintptr
	length  int
}

// Clone 复制实体状态并隔离可变引用；无法安全复制的私有引用返回 Unsupported。
// time.Time 的内部位置资料按不可变值保留。
func Clone[T any](entity T) (T, error) {
	var zero T
	value := reflect.ValueOf(entity)
	if !value.IsValid() {
		return zero, errors.NewCode(errors.InvalidInput, "entity cannot be nil")
	}
	out, err := cloneValue(value, map[clonePointer]reflect.Value{})
	if err != nil {
		return zero, err
	}
	return out.Interface().(T), nil
}

func cloneValue(value reflect.Value, pointers map[clonePointer]reflect.Value) (reflect.Value, error) {
	switch value.Kind() {
	case reflect.Ptr:
		if value.IsNil() {
			return reflect.Zero(value.Type()), nil
		}
		key := clonePointer{typ: value.Type(), pointer: value.Pointer()}
		if out, exists := pointers[key]; exists {
			return out, nil
		}
		out := reflect.New(value.Type().Elem())
		pointers[key] = out
		element, err := cloneValue(value.Elem(), pointers)
		if err != nil {
			return reflect.Value{}, err
		}
		out.Elem().Set(element)
		return out, nil
	case reflect.Struct:
		if value.Type() == reflect.TypeOf(time.Time{}) {
			return value, nil
		}
		out := reflect.New(value.Type()).Elem()
		out.Set(value)
		for index := 0; index < value.NumField(); index++ {
			if value.Type().Field(index).PkgPath != "" {
				if hasPrivateReference(value.Field(index)) {
					return reflect.Value{}, errors.NewCode(errors.Unsupported, "entity state contains a private mutable reference").
						WithContext("type", value.Type().String()).WithContext("field", value.Type().Field(index).Name)
				}
				continue
			}
			field, err := cloneValue(value.Field(index), pointers)
			if err != nil {
				return reflect.Value{}, err
			}
			out.Field(index).Set(field)
		}
		return out, nil
	case reflect.Interface:
		if value.IsNil() {
			return reflect.Zero(value.Type()), nil
		}
		element, err := cloneValue(value.Elem(), pointers)
		if err != nil {
			return reflect.Value{}, err
		}
		out := reflect.New(value.Type()).Elem()
		out.Set(element)
		return out, nil
	case reflect.Slice:
		if value.IsNil() {
			return reflect.Zero(value.Type()), nil
		}
		key := clonePointer{typ: value.Type(), pointer: value.Pointer(), length: value.Len()}
		if out, exists := pointers[key]; exists {
			return out, nil
		}
		out := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		pointers[key] = out
		for index := 0; index < value.Len(); index++ {
			element, err := cloneValue(value.Index(index), pointers)
			if err != nil {
				return reflect.Value{}, err
			}
			out.Index(index).Set(element)
		}
		return out, nil
	case reflect.Map:
		if value.IsNil() {
			return reflect.Zero(value.Type()), nil
		}
		key := clonePointer{typ: value.Type(), pointer: uintptr(value.UnsafePointer())}
		if out, exists := pointers[key]; exists {
			return out, nil
		}
		out := reflect.MakeMapWithSize(value.Type(), value.Len())
		pointers[key] = out
		iter := value.MapRange()
		for iter.Next() {
			element, err := cloneValue(iter.Value(), pointers)
			if err != nil {
				return reflect.Value{}, err
			}
			mapKey, err := cloneValue(iter.Key(), pointers)
			if err != nil {
				return reflect.Value{}, err
			}
			out.SetMapIndex(mapKey, element)
		}
		return out, nil
	case reflect.Array:
		out := reflect.New(value.Type()).Elem()
		for index := 0; index < value.Len(); index++ {
			element, err := cloneValue(value.Index(index), pointers)
			if err != nil {
				return reflect.Value{}, err
			}
			out.Index(index).Set(element)
		}
		return out, nil
	case reflect.Chan, reflect.Func, reflect.UnsafePointer:
		if !value.IsNil() {
			return reflect.Value{}, errors.NewCode(errors.Unsupported, "entity state contains an uncopyable reference").WithContext("type", value.Type().String())
		}
		return value, nil
	default:
		return value, nil
	}
}

func hasPrivateReference(value reflect.Value) bool {
	if value.Type() == reflect.TypeOf(time.Time{}) {
		return false
	}
	switch value.Kind() {
	case reflect.Ptr, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return !value.IsNil()
	case reflect.Interface:
		return !value.IsNil() && hasPrivateReference(value.Elem())
	case reflect.Struct:
		for i := 0; i < value.NumField(); i++ {
			if hasPrivateReference(value.Field(i)) {
				return true
			}
		}
	case reflect.Array:
		for i := 0; i < value.Len(); i++ {
			if hasPrivateReference(value.Index(i)) {
				return true
			}
		}
	}
	return false
}
