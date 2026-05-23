package upcast

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"

	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/registry"
	"gochen/messaging"
)

// UpgradeEventPayload 统一的事件载荷升级入口（显式注入 registry/upgraders）。
//
// 说明：
// - 行为：
// - - 若事件载荷为 map[string]any，先通过 UpgradeEventData 执行基于数据的升级，
// - 再使用 registry 反序列化为强类型并回写到事件载荷；同时更新事件的 SchemaVersion。
// - - 若事件载荷为 json.RawMessage/[]byte/string（JSON bytes），会先在必要时解码为 map，
// - 然后复用同样的升级与 hydration 逻辑；当 schema 已是最新且事件类型已注册时，
// - 会优先直接从 JSON bytes 反序列化为强类型，避免 map 中间态。
// - - 若载荷已为强类型，则不做处理。
// - - 返回升级后的事件指针（与入参相同实例），便于链式处理。
// - 说明：当前推荐的 Schema 演进路径是“map 级升级 + hydration”：
// - - 通过 registry.RegisterWithVersion 声明最新 schemaVersion；
// - - 通过 upgraders.Register 注册升级链；
// - - 在消费边界调用 `UpgradeEventPayload` 完成升级与强类型反序列化。
//
// 参数：
// - reg：事件 registry（用于 schemaVersion 与 hydration）
func UpgradeEventPayload[ID comparable](
	ctx context.Context,
	reg *registry.Registry,
	upgraders *UpgraderRegistry,
	evt *eventing.Event[ID],
) (*eventing.Event[ID], error) {
	if evt == nil {
		return nil, nil
	}
	if reg == nil {
		return nil, errors.NewCode(errors.InvalidInput, "event registry cannot be nil")
	}
	if upgraders == nil {
		return nil, errors.NewCode(errors.InvalidInput, "event upgrader registry cannot be nil")
	}
	if !reg.HasEvent(evt.GetType()) {
		return nil, errors.NewCode(errors.NotFound, "unknown event type").WithContext("event_type", evt.GetType())
	}

	payload := messaging.PayloadValue(evt.GetPayload())
	switch typed := payload.(type) {
	case map[string]any:
		return upgradeEventPayloadFromMap(reg, upgraders, evt, typed)
	case json.RawMessage:
		return upgradeEventPayloadFromJSONBytes(reg, upgraders, evt, []byte(typed))
	case []byte:
		return upgradeEventPayloadFromJSONBytes(reg, upgraders, evt, typed)
	case string:
		return upgradeEventPayloadFromJSONBytes(reg, upgraders, evt, []byte(typed))
	default:
		return evt, nil
	}
}

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

// upgradeEventPayloadFromJSONBytes 执行对应操作。
func upgradeEventPayloadFromJSONBytes[ID comparable](
	reg *registry.Registry,
	upgraders *UpgraderRegistry,
	evt *eventing.Event[ID],
	jsonBytes []byte,
) (*eventing.Event[ID], error) {
	if evt == nil {
		return nil, nil
	}
	trimmed := bytes.TrimSpace(jsonBytes)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return evt, nil
	}

	// 优先快路径：schema 已是最新 + 已注册类型时，直接从 JSON bytes 解码为强类型，
	// 避免 “bytes -> map -> marshal -> decode” 的二次转换。
	targetSchema := reg.EventSchemaVersion(evt.GetType())
	if targetSchema <= 0 {
		targetSchema = 1
	}
	currentSchema := evt.EventSchemaVersion()
	if currentSchema <= 0 {
		currentSchema = 1
	}
	if currentSchema >= targetSchema {
		typed, err := reg.DeserializeWithUseNumber(evt.GetType(), trimmed)
		if err == nil {
			evt.Payload = messaging.NewPayload(typed)
			return evt, nil
		}
	}

	var payloadMap map[string]any
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	if err := decoder.Decode(&payloadMap); err != nil {
		return evt, err
	}
	return upgradeEventPayloadFromMap(reg, upgraders, evt, payloadMap)
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

func eventSchemaVersion(evt eventing.IEvent) int {
	if evt == nil {
		return 1
	}
	if versioned, ok := evt.(interface{ EventSchemaVersion() int }); ok {
		if version := versioned.EventSchemaVersion(); version > 0 {
			return version
		}
	}
	return 1
}

func normalizeTypedPayload(payload any) (any, bool, error) {
	value := reflect.ValueOf(payload)
	if !value.IsValid() {
		return nil, false, nil
	}
	if value.Kind() == reflect.Struct {
		ptr := reflect.New(value.Type())
		ptr.Elem().Set(value)
		return ptr.Interface(), true, nil
	}
	return payload, true, nil
}

func upgradeEventPayloadFromMap[ID comparable](
	reg *registry.Registry,
	upgraders *UpgraderRegistry,
	evt *eventing.Event[ID],
	dataMap map[string]any,
) (*eventing.Event[ID], error) {
	if evt == nil {
		return nil, nil
	}
	if dataMap == nil {
		return evt, nil
	}

	upgraded, ver, err := UpgradeEventData(reg, upgraders, evt.GetType(), evt.EventSchemaVersion(), dataMap)
	if err != nil {
		// 尽量保留可调试性：即便升级失败，也回写 map 载荷，避免调用方拿到原始 RawMessage/空载荷。
		evt.Payload = messaging.NewPayload(dataMap)
		return evt, err
	}

	typed, derr := reg.DeserializeFromMap(evt.GetType(), upgraded)
	if derr != nil {
		evt.Payload = messaging.NewPayload(upgraded)
		if ver > 0 {
			evt.SchemaVersion = ver
		}
		return evt, derr
	}
	evt.Payload = messaging.NewPayload(typed)
	if ver > 0 {
		evt.SchemaVersion = ver
	}
	return evt, nil
}
