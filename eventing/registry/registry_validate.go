package registry

import (
	"reflect"

	"gochen/errors"
)

// ValidateEvent 校验事件实例是否符合名称对应的已注册 Go 类型。
// 仅接受工厂登记的指针类型或其一层解引用的值类型，不按可转换性或 JSON 形状匹配。
// 未注册名称返回 NotFound；空实例或类型不符返回 InvalidInput，校验不会调用工厂或修改实例。
func (r *Registry) ValidateEvent(eventType string, event any) error {
	if r == nil {
		return errors.NewCode(errors.InvalidInput, "event registry cannot be nil")
	}
	r.mutex.RLock()
	registeredType, exists := r.eventTypes[eventType]
	r.mutex.RUnlock()
	if !exists {
		return errors.NewCode(errors.NotFound, "unknown event type").WithContext("event_type", eventType)
	}
	if isNilInstance(event) {
		return errors.NewCode(errors.InvalidInput, "event cannot be nil").WithContext("event_type", eventType)
	}
	actualType := reflect.TypeOf(event)
	if actualType != registeredType && actualType != registeredType.Elem() {
		return errors.NewCode(errors.InvalidInput, "event does not match registered Go type").
			WithContext("event_type", eventType).
			WithContext("registered_type", registeredType.String()).
			WithContext("actual_type", actualType.String())
	}
	return nil
}
