// Package registry 提供事件类型注册表，用于事件的反序列化。
package registry

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sync"

	"gochen/codec/jsoncodec"
	"gochen/errors"
)

// EventFactory 事件工厂函数。
type EventFactory func() any

// Registry 事件注册表。
type Registry struct {
	eventTypes map[string]reflect.Type
	factories  map[string]EventFactory
	versions   map[string]int
	mutex      sync.RWMutex
}

// NewRegistry 创建一个空的事件类型注册表。
func NewRegistry() *Registry {
	return &Registry{
		eventTypes: make(map[string]reflect.Type),
		factories:  make(map[string]EventFactory),
		versions:   make(map[string]int),
	}
}

// Register 以默认 schema 版本 `1` 注册事件类型。
func (r *Registry) Register(eventType string, factory EventFactory) error {
	return r.RegisterWithVersion(eventType, 1, factory)
}

// RegisterWithVersion 注册带 schema 版本的事件类型。
func (r *Registry) RegisterWithVersion(eventType string, schemaVersion int, factory EventFactory) error {
	if eventType == "" {
		return errors.NewCode(errors.InvalidInput, "event type cannot be empty")
	}
	if factory == nil {
		return errors.NewCode(errors.InvalidInput, "event factory cannot be nil").WithContext("event_type", eventType)
	}
	if schemaVersion <= 0 {
		return errors.NewCode(errors.InvalidInput, "schema version must be greater than 0").WithContext("event_type", eventType)
	}

	r.mutex.Lock()
	defer r.mutex.Unlock()

	if _, exists := r.eventTypes[eventType]; exists {
		return errors.NewCode(errors.Conflict, "event type already registered").WithContext("event_type", eventType)
	}

	instance := factory()
	if instance == nil {
		return errors.NewCode(errors.InvalidInput, "event factory returned nil").WithContext("event_type", eventType)
	}

	r.eventTypes[eventType] = reflect.TypeOf(instance)
	r.factories[eventType] = factory
	r.versions[eventType] = schemaVersion
	return nil
}

// Unregister 移除一个事件类型及其工厂、版本信息。
func (r *Registry) Unregister(eventType string) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	delete(r.eventTypes, eventType)
	delete(r.factories, eventType)
	delete(r.versions, eventType)
}

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

	// 先将 map 序列化为 JSON bytes
	// 注意：若 data 来自 json.Decoder.UseNumber() 的解码（数字为 json.Number），直接 json.Marshal 会把数字编码为 JSON string。
	// 这里使用 jsoncodec.MarshalPreserveNumber 将 json.Number 规范化为可按 number 输出的 RawNumber，避免精度与类型语义丢失。
	jsonBytes, err := jsoncodec.MarshalPreserveNumber(data)
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

// HasEvent 判断某个事件类型是否已经注册。
func (r *Registry) HasEvent(eventType string) bool {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	_, exists := r.eventTypes[eventType]
	return exists
}

func (r *Registry) RegisteredTypes() []string {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	types := make([]string, 0, len(r.eventTypes))
	for eventType := range r.eventTypes {
		types = append(types, eventType)
	}

	return types
}

// EventSchemaVersion 返回事件类型当前登记的 schema 版本。
func (r *Registry) EventSchemaVersion(eventType string) int {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	if version, ok := r.versions[eventType]; ok && version > 0 {
		return version
	}
	return 1
}

// NOTE:
// - 本包仅提供可注入的 *Registry，不提供任何包级全局默认值或 Global API；
// - 调用方应在组合根显式创建 Registry 并注入到需要进行 payload hydration 的组件中。
