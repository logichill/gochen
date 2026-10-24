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
// - - 成功时返回升级后的事件指针（与入参相同实例），便于链式处理；升级失败时可能返回携带调试载荷的副本。
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
	if ctx == nil {
		return evt, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if err := ctx.Err(); err != nil {
		return evt, err
	}
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
		debugEvt := *evt
		debugEvt.Payload = messaging.NewPayload(dataMap)
		return &debugEvt, err
	}

	typed, derr := reg.DeserializeFromMap(evt.GetType(), upgraded)
	if derr != nil {
		// 失败时绝不修改原始事件：构造携带调试载荷的副本返回，
		// 保证重试/多订阅者链路拿到未被污染的原始事件。
		debugEvt := *evt
		debugEvt.Payload = messaging.NewPayload(upgraded)
		if ver > 0 {
			debugEvt.SchemaVersion = ver
		}
		return &debugEvt, derr
	}
	evt.Payload = messaging.NewPayload(typed)
	if ver > 0 {
		evt.SchemaVersion = ver
	}
	return evt, nil
}
