package httpx

import (
	"encoding/json"
	"reflect"
)

// JSONBody 封装 JSON 响应载荷，避免在公共接口上直接暴露裸 any。
type JSONBody struct {
	value any
}

// JSONValue 创建 JSONBody。
func JSONValue[T any](value T) JSONBody {
	return JSONBody{value: value}
}

// IsNil 判断载荷是否为空。
func (b JSONBody) IsNil() bool { return isNilLike(b.value) }

// MarshalJSON 让适配层可以直接序列化 JSONBody，而不需要暴露裸 any。
func (b JSONBody) MarshalJSON() ([]byte, error) { return json.Marshal(b.value) }

// JSONBodyAs 尝试把 JSONBody 断言为目标类型。
func JSONBodyAs[T any](body JSONBody) (T, bool) {
	var zero T
	if body.value == nil {
		return zero, false
	}
	typed, ok := body.value.(T)
	if !ok {
		return zero, false
	}
	return typed, true
}

// ContextValue 封装请求级存储值，避免公共接口直接返回裸 any。
type ContextValue struct {
	value any
}

// ValueOf 创建 ContextValue。
func ValueOf[T any](value T) ContextValue {
	return ContextValue{value: value}
}

// IsNil 判断值是否为空。
func (v ContextValue) IsNil() bool { return isNilLike(v.value) }

func isNilLike(value any) bool {
	if value == nil {
		return true
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Map, reflect.Pointer, reflect.Interface, reflect.Slice:
		return rv.IsNil()
	default:
		return false
	}
}

// ValueAs 尝试把 ContextValue 断言为目标类型。
func ValueAs[T any](value ContextValue) (T, bool) {
	var zero T
	if value.value == nil {
		return zero, false
	}
	typed, ok := value.value.(T)
	if !ok {
		return zero, false
	}
	return typed, true
}
