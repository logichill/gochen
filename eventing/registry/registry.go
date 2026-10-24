// Package registry 提供事件类型注册表，用于事件的反序列化。
package registry

import (
	"reflect"
	"sync"

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
	if isNilInstance(instance) {
		return errors.NewCode(errors.InvalidInput, "event factory returned nil").WithContext("event_type", eventType)
	}
	// 工厂必须返回指针：反序列化时 registry 会把工厂返回值直接作为
	// json.Unmarshal 目标，非指针会在运行期报 "Unmarshal(non-pointer ...)"，
	// 导致该类型全部事件解码失败。此处 fail-fast。
	if reflect.TypeOf(instance).Kind() != reflect.Pointer {
		return errors.NewCode(errors.InvalidInput, "event factory must return a pointer type").
			WithContext("event_type", eventType).
			WithContext("factory_return_type", reflect.TypeOf(instance).String())
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

// HasEvent 判断某个事件类型是否已经注册。
func (r *Registry) HasEvent(eventType string) bool {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	_, exists := r.eventTypes[eventType]
	return exists
}

func (r *Registry) ListRegisteredTypes() []string {
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
