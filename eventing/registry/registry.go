// Package registry 提供事件类型注册表，用于事件的反序列化
package registry

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
)

// EventFactory 事件工厂函数
type EventFactory func() interface{}

// Registry 事件注册表
type Registry struct {
	eventTypes map[string]reflect.Type
	factories  map[string]EventFactory
	versions   map[string]int
	mutex      sync.RWMutex
}

// NewRegistry 创建注册表
func NewRegistry() *Registry {
	return &Registry{
		eventTypes: make(map[string]reflect.Type),
		factories:  make(map[string]EventFactory),
		versions:   make(map[string]int),
	}
}

// Register 注册事件类型
func (r *Registry) Register(eventType string, factory EventFactory) error {
	return r.RegisterWithVersion(eventType, 1, factory)
}

// RegisterWithVersion 注册带模式版本的事件类型
func (r *Registry) RegisterWithVersion(eventType string, schemaVersion int, factory EventFactory) error {
	if eventType == "" {
		return fmt.Errorf("event type cannot be empty")
	}
	if factory == nil {
		return fmt.Errorf("event factory cannot be nil for type %s", eventType)
	}
	if schemaVersion <= 0 {
		return fmt.Errorf("schema version must be greater than 0 for type %s", eventType)
	}

	r.mutex.Lock()
	defer r.mutex.Unlock()

	if _, exists := r.eventTypes[eventType]; exists {
		return fmt.Errorf("event type already registered: %s", eventType)
	}

	instance := factory()
	if instance == nil {
		return fmt.Errorf("event factory returned nil for type %s", eventType)
	}

	r.eventTypes[eventType] = reflect.TypeOf(instance)
	r.factories[eventType] = factory
	r.versions[eventType] = schemaVersion
	return nil
}

// MustRegister 注册事件类型（失败 panic）
func (r *Registry) MustRegister(eventType string, factory EventFactory) {
	if err := r.Register(eventType, factory); err != nil {
		panic(err)
	}
}

// MustRegisterWithVersion 注册带版本事件类型（失败 panic）
func (r *Registry) MustRegisterWithVersion(eventType string, schemaVersion int, factory EventFactory) {
	if err := r.RegisterWithVersion(eventType, schemaVersion, factory); err != nil {
		panic(err)
	}
}

// Unregister 取消注册
func (r *Registry) Unregister(eventType string) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	delete(r.eventTypes, eventType)
	delete(r.factories, eventType)
	delete(r.versions, eventType)
}

// Deserialize 通过注册表反序列化事件数据
func (r *Registry) Deserialize(eventType string, data []byte) (interface{}, error) {
	r.mutex.RLock()
	factory, exists := r.factories[eventType]
	r.mutex.RUnlock()

	if !exists {
		return nil, fmt.Errorf("unknown event type: %s", eventType)
	}

	instance := factory()
	if err := json.Unmarshal(data, instance); err != nil {
		return nil, fmt.Errorf("failed to deserialize event %s: %w", eventType, err)
	}
	return instance, nil
}

// DeserializeFromMap 将 map 数据转换为强类型事件
func (r *Registry) DeserializeFromMap(eventType string, data map[string]interface{}) (interface{}, error) {
	if data == nil {
		return nil, fmt.Errorf("event data map cannot be nil")
	}

	bytes, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal event map for %s: %w", eventType, err)
	}

	return r.Deserialize(eventType, bytes)
}

// HasEvent 检查事件类型是否已注册
func (r *Registry) HasEvent(eventType string) bool {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	_, exists := r.eventTypes[eventType]
	return exists
}

// GetRegisteredTypes 获取所有已注册的事件类型
func (r *Registry) GetRegisteredTypes() []string {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	types := make([]string, 0, len(r.eventTypes))
	for eventType := range r.eventTypes {
		types = append(types, eventType)
	}

	return types
}

// GetSchemaVersion 获取事件类型的最新模式版本
func (r *Registry) GetSchemaVersion(eventType string) int {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	if version, ok := r.versions[eventType]; ok && version > 0 {
		return version
	}
	return 1
}

var globalRegistry = NewRegistry()

// RegisterGlobal 注册到全局注册表
func RegisterGlobal(eventType string, factory EventFactory) error {
	return globalRegistry.Register(eventType, factory)
}

// MustRegisterGlobal 注册（失败 panic）
func MustRegisterGlobal(eventType string, factory EventFactory) {
	globalRegistry.MustRegister(eventType, factory)
}

// RegisterGlobalWithVersion 注册带版本的事件类型
func RegisterGlobalWithVersion(eventType string, schemaVersion int, factory EventFactory) error {
	return globalRegistry.RegisterWithVersion(eventType, schemaVersion, factory)
}

// MustRegisterGlobalWithVersion 注册带版本（失败 panic）
func MustRegisterGlobalWithVersion(eventType string, schemaVersion int, factory EventFactory) {
	globalRegistry.MustRegisterWithVersion(eventType, schemaVersion, factory)
}

// DeserializeGlobal 通过全局注册表反序列化
func DeserializeGlobal(eventType string, data []byte) (interface{}, error) {
	return globalRegistry.Deserialize(eventType, data)
}

// DeserializeMapGlobal 通过全局注册表从 map 反序列化
func DeserializeMapGlobal(eventType string, data map[string]interface{}) (interface{}, error) {
	return globalRegistry.DeserializeFromMap(eventType, data)
}

// HasEventGlobal 检查全局注册表
func HasEventGlobal(eventType string) bool {
	return globalRegistry.HasEvent(eventType)
}

// GetRegisteredTypesGlobal 获取全局注册的类型
func GetRegisteredTypesGlobal() []string {
	return globalRegistry.GetRegisteredTypes()
}

// GetSchemaVersionGlobal 获取全局模式版本
func GetSchemaVersionGlobal(eventType string) int {
	return globalRegistry.GetSchemaVersion(eventType)
}

// 保持与旧包一致的别名

func RegisterEventType(eventType string, factory EventFactory) error {
	return RegisterGlobal(eventType, factory)
}

func MustRegisterEventType(eventType string, factory EventFactory) {
	MustRegisterGlobal(eventType, factory)
}

func RegisterEventTypeWithVersion(eventType string, schemaVersion int, factory EventFactory) error {
	return RegisterGlobalWithVersion(eventType, schemaVersion, factory)
}

func MustRegisterEventTypeWithVersion(eventType string, schemaVersion int, factory EventFactory) {
	MustRegisterGlobalWithVersion(eventType, schemaVersion, factory)
}

func DeserializeEvent(eventType string, data []byte) (interface{}, error) {
	return DeserializeGlobal(eventType, data)
}

func DeserializeEventFromMap(eventType string, data map[string]interface{}) (interface{}, error) {
	return DeserializeMapGlobal(eventType, data)
}

func IsEventTypeRegistered(eventType string) bool {
	return HasEventGlobal(eventType)
}

func GetAllRegisteredEventTypes() []string {
	return GetRegisteredTypesGlobal()
}

func GetEventSchemaVersion(eventType string) int {
	return GetSchemaVersionGlobal(eventType)
}
