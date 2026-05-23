package messaging

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
)

// Payload 封装消息/事件载荷，避免核心协议层直接暴露裸 any。
type Payload struct {
	value any
}

// NewPayload 创建载荷封装。
func NewPayload(value any) Payload {
	return Payload{value: value}
}

// Type 返回底层值的反射类型；nil 载荷返回 nil。
func (p Payload) Type() reflect.Type {
	if p.value == nil {
		return nil
	}
	return reflect.TypeOf(p.value)
}

// TypeName 返回底层值的类型名；nil 载荷返回 "<nil>"。
func (p Payload) TypeName() string {
	if typ := p.Type(); typ != nil {
		return typ.String()
	}
	return "<nil>"
}

// IsNil 判断载荷是否为空。
func (p Payload) IsNil() bool {
	if p.value == nil {
		return true
	}
	rv := reflect.ValueOf(p.value)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Map, reflect.Pointer, reflect.Interface, reflect.Slice:
		return rv.IsNil()
	default:
		return false
	}
}

// DecodeTo 将载荷解码/赋值到目标对象。
func (p Payload) DecodeTo(target any) error {
	if target == nil {
		return fmt.Errorf("payload decode target is nil")
	}

	if p.value == nil {
		v := reflect.ValueOf(target)
		if v.Kind() != reflect.Ptr || v.IsNil() {
			return fmt.Errorf("payload decode target must be a non-nil pointer")
		}
		v.Elem().Set(reflect.Zero(v.Elem().Type()))
		return nil
	}

	targetValue := reflect.ValueOf(target)
	if targetValue.Kind() != reflect.Ptr || targetValue.IsNil() {
		return fmt.Errorf("payload decode target must be a non-nil pointer")
	}

	sourceValue := reflect.ValueOf(p.value)
	if sourceValue.Type().AssignableTo(targetValue.Elem().Type()) {
		targetValue.Elem().Set(sourceValue)
		return nil
	}

	data, err := json.Marshal(p.value)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	return decoder.Decode(target)
}

// MarshalJSON 直接序列化底层值。
func (p Payload) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.value)
}

// UnmarshalJSON 将 JSON 反序列化为宽松载荷，并保留数字精度。
func (p *Payload) UnmarshalJSON(data []byte) error {
	if p == nil {
		return fmt.Errorf("payload is nil")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	p.value = value
	return nil
}

// PayloadValue 返回底层值，供适配/序列化层在受控位置使用。
func PayloadValue(payload Payload) any {
	return payload.value
}

// PayloadAs 尝试将载荷断言/解码为目标类型。
func PayloadAs[T any](payload Payload) (T, bool) {
	var zero T
	if payload.value == nil {
		return zero, false
	}
	if typed, ok := payload.value.(T); ok {
		return typed, true
	}
	var out T
	if err := payload.DecodeTo(&out); err != nil {
		return zero, false
	}
	return out, true
}
