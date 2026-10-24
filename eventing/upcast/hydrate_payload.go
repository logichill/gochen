package upcast

import (
	"bytes"
	"encoding/json"
	"reflect"

	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/registry"
	"gochen/messaging"
)

// HydrateEventPayload 把事件载荷升级并反序列化为消费侧可用的强类型对象。
//
// 说明：
//   - 该函数面向事件总线、投影、readmodel 等消费边界，接受通用 eventing.IEvent；
//   - 若 payload 已经是强类型对象，直接返回；若是 struct 值，会规范化为指针返回；
//   - 若 payload 是 map/JSON bytes/string，则使用事件自身的 EventSchemaVersion 执行 upcast，
//     再通过 registry 反序列化为强类型对象；
//   - ok=false 表示没有可消费 payload，调用方可直接跳过。
func HydrateEventPayload(reg *registry.Registry, upgraders *UpgraderRegistry, evt eventing.IEvent) (any, bool, error) {
	if evt == nil {
		return nil, false, nil
	}

	payload := messaging.PayloadValue(evt.GetPayload())
	if payload == nil {
		return nil, false, nil
	}

	switch typed := payload.(type) {
	case map[string]any:
		return hydrateEventPayloadFromMap(reg, upgraders, evt, typed)
	case json.RawMessage:
		return hydrateEventPayloadFromJSONBytes(reg, upgraders, evt, []byte(typed))
	case []byte:
		return hydrateEventPayloadFromJSONBytes(reg, upgraders, evt, typed)
	case string:
		return hydrateEventPayloadFromJSONBytes(reg, upgraders, evt, []byte(typed))
	default:
		return normalizeTypedPayload(payload)
	}
}

// DecodeEventPayload 把事件载荷升级/反序列化，并校验为目标类型。
func DecodeEventPayload[T any](reg *registry.Registry, upgraders *UpgraderRegistry, evt eventing.IEvent) (T, bool, error) {
	var zero T
	payload, ok, err := HydrateEventPayload(reg, upgraders, evt)
	if !ok || err != nil {
		return zero, ok, err
	}
	if payload == nil {
		return zero, false, nil
	}

	if typed, ok := payload.(T); ok {
		return typed, true, nil
	}

	targetType := reflect.TypeOf((*T)(nil)).Elem()
	payloadValue := reflect.ValueOf(payload)
	if !payloadValue.IsValid() {
		return zero, false, nil
	}

	if payloadValue.Type().AssignableTo(targetType) {
		return payloadValue.Interface().(T), true, nil
	}
	if payloadValue.Type().ConvertibleTo(targetType) {
		return payloadValue.Convert(targetType).Interface().(T), true, nil
	}
	if targetType.Kind() == reflect.Ptr && payloadValue.Type().AssignableTo(targetType.Elem()) {
		ptr := reflect.New(targetType.Elem())
		ptr.Elem().Set(payloadValue)
		return ptr.Interface().(T), true, nil
	}
	if payloadValue.Kind() == reflect.Ptr && !payloadValue.IsNil() && payloadValue.Elem().Type().AssignableTo(targetType) {
		return payloadValue.Elem().Interface().(T), true, nil
	}

	return zero, true, errors.NewCode(errors.InvalidInput, "event payload type mismatch").
		WithContext("event_type", evt.GetType()).
		WithContext("expected", targetType.String()).
		WithContext("actual", payloadValue.Type().String())
}

func hydrateEventPayloadFromJSONBytes(
	reg *registry.Registry,
	upgraders *UpgraderRegistry,
	evt eventing.IEvent,
	jsonBytes []byte,
) (any, bool, error) {
	trimmed := bytes.TrimSpace(jsonBytes)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, false, nil
	}
	if err := requireEventRegistry(reg, evt.GetType()); err != nil {
		return nil, true, err
	}

	targetSchema := reg.EventSchemaVersion(evt.GetType())
	if targetSchema <= 0 {
		targetSchema = 1
	}
	currentSchema := eventSchemaVersion(evt)
	if currentSchema >= targetSchema {
		typed, err := reg.DeserializeWithUseNumber(evt.GetType(), trimmed)
		if err != nil {
			return nil, true, err
		}
		return typed, true, nil
	}
	if upgraders == nil {
		return nil, true, errors.NewCode(errors.InvalidInput, "event upgrader registry cannot be nil").WithContext("event_type", evt.GetType())
	}

	var payloadMap map[string]any
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	if err := decoder.Decode(&payloadMap); err != nil {
		return nil, true, err
	}
	return hydrateEventPayloadFromMap(reg, upgraders, evt, payloadMap)
}

func hydrateEventPayloadFromMap(
	reg *registry.Registry,
	upgraders *UpgraderRegistry,
	evt eventing.IEvent,
	dataMap map[string]any,
) (any, bool, error) {
	if dataMap == nil {
		return nil, false, nil
	}
	if err := requireEventRegistry(reg, evt.GetType()); err != nil {
		return nil, true, err
	}

	targetSchema := reg.EventSchemaVersion(evt.GetType())
	if targetSchema <= 0 {
		targetSchema = 1
	}
	if eventSchemaVersion(evt) >= targetSchema {
		typed, err := reg.DeserializeFromMap(evt.GetType(), dataMap)
		if err != nil {
			return nil, true, err
		}
		return typed, true, nil
	}
	if upgraders == nil {
		return nil, true, errors.NewCode(errors.InvalidInput, "event upgrader registry cannot be nil").WithContext("event_type", evt.GetType())
	}

	upgraded, _, err := UpgradeEventData(reg, upgraders, evt.GetType(), eventSchemaVersion(evt), dataMap)
	if err != nil {
		return nil, true, err
	}
	typed, err := reg.DeserializeFromMap(evt.GetType(), upgraded)
	if err != nil {
		return nil, true, err
	}
	return typed, true, nil
}

func requireEventRegistry(reg *registry.Registry, eventType string) error {
	if reg == nil {
		return errors.NewCode(errors.InvalidInput, "event registry cannot be nil").WithContext("event_type", eventType)
	}
	if !reg.HasEvent(eventType) {
		return errors.NewCode(errors.NotFound, "unknown event type").WithContext("event_type", eventType)
	}
	return nil
}
