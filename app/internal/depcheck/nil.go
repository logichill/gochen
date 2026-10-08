// Package depcheck 校验应用层接口依赖的动态值。
package depcheck

import "reflect"

// IsNil 同时识别 nil 接口与包含 nil 指针、函数等动态值的接口。
func IsNil(value any) bool {
	if value == nil {
		return true
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return rv.IsNil()
	}
	return false
}
