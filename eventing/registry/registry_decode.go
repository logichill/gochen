package registry

import (
	"bytes"
	"encoding/json"
	"reflect"

	"gochen/codec/jsoncodec"
	"gochen/errors"
)

// Deserialize 按事件类型把 JSON 字节反序列化为强类型事件对象。
func (r *Registry) Deserialize(eventType string, data []byte) (any, error) {
	r.mutex.RLock()
	factory, exists := r.factories[eventType]
	r.mutex.RUnlock()

	if !exists {
		return nil, errors.NewCode(errors.NotFound, "unknown event type").WithContext("event_type", eventType)
	}

	instance := factory()
	if err := json.Unmarshal(data, instance); err != nil {
		return nil, errors.Wrap(err, errors.InvalidInput, "failed to deserialize event").WithContext("event_type", eventType)
	}
	return instance, nil
}

// DeserializeWithUseNumber 在反序列化时保留数字精度。
func (r *Registry) DeserializeWithUseNumber(eventType string, data []byte) (any, error) {
	r.mutex.RLock()
	factory, exists := r.factories[eventType]
	r.mutex.RUnlock()

	if !exists {
		return nil, errors.NewCode(errors.NotFound, "unknown event type").WithContext("event_type", eventType)
	}

	instance := factory()
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(instance); err != nil {
		return nil, errors.Wrap(err, errors.InvalidInput, "failed to deserialize event").WithContext("event_type", eventType)
	}
	return instance, nil
}

// DeserializeFromMap 把通用 map 载荷转换为强类型事件，并尽量保留数字精度。
func (r *Registry) DeserializeFromMap(eventType string, data map[string]any) (any, error) {
	if data == nil {
		return nil, errors.NewCode(errors.InvalidInput, "event data map cannot be nil")
	}

	r.mutex.RLock()
	factory, exists := r.factories[eventType]
	r.mutex.RUnlock()

	if !exists {
		return nil, errors.NewCode(errors.NotFound, "unknown event type").WithContext("event_type", eventType)
	}

	// Encode 直接保留 UseNumber 解码得到的数字表示，不经过浮点数转换。
	jsonBytes, err := jsoncodec.Encode(data)
	if err != nil {
		return nil, errors.Wrap(err, errors.Internal, "failed to marshal event map").WithContext("event_type", eventType)
	}

	// 使用 json.Decoder + UseNumber() 反序列化，保持数字精度
	instance := factory()
	decoder := json.NewDecoder(bytes.NewReader(jsonBytes))
	decoder.UseNumber() // 关键：数字保持为 json.Number 而非 float64
	if err := decoder.Decode(instance); err != nil {
		return nil, errors.Wrap(err, errors.InvalidInput, "failed to deserialize event").WithContext("event_type", eventType)
	}
	return instance, nil
}

// isNilInstance 判断工厂返回的实例是否为空，包括底层指针为 nil 的 typed-nil 接口值。
// 反序列化会把实例直接作为 json.Unmarshal 的目标，typed-nil 实例必然导致运行期失败，
// 故在注册期统一拦截（对齐 eventing/bus.isNilEventHandler 的反射式判别）。
func isNilInstance(instance any) bool {
	if instance == nil {
		return true
	}
	value := reflect.ValueOf(instance)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
