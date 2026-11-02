package meta

import (
	"reflect"

	"gochen/errors"
)

type clonePointer struct {
	typ     reflect.Type
	pointer uintptr
	length  int
}

// Clone 复制实体状态，保留结构体内部值并隔离导出字段的可变引用。
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
		out := reflect.New(value.Type()).Elem()
		out.Set(value)
		for index := 0; index < value.NumField(); index++ {
			if value.Type().Field(index).PkgPath != "" {
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
	default:
		return value, nil
	}
}
