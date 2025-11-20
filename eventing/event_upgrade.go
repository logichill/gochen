package eventing

import (
	"context"

	"gochen/eventing/registry"
)

// UpgradeEventPayload 统一的事件载荷升级入口。
//
// 行为：
//   - 若事件载荷为 map[string]any，先通过 UpgradeEventData 执行基于数据的升级，
//     再使用 registry 反序列化为强类型并回写到事件载荷；同时更新事件的 SchemaVersion。
//   - 若载荷已为强类型，则不做处理（对象级升级由独立的 upgrader 包负责，避免循环依赖）。
//   - 返回升级后的事件指针（与入参相同实例），便于链式处理。
//
// 说明：对象级升级链（eventing/upgrader/*）与本方法相互独立，调用方可在合适
// 场景（不产生循环依赖处）组合两者以实现完整的演进策略。
func UpgradeEventPayload(ctx context.Context, evt *Event) (*Event, error) {
	if evt == nil {
		return nil, nil
	}
	// 仅在载荷为 map 时执行数据级升级 + 强类型反序列化
	if dataMap, ok := evt.GetPayload().(map[string]any); ok && dataMap != nil {
		upgraded, ver, err := UpgradeEventData(evt.GetType(), evt.GetSchemaVersion(), dataMap)
		if err != nil {
			return evt, err
		}
		typed, err := registry.DeserializeEventFromMap(evt.GetType(), upgraded)
		if err != nil {
			return evt, err
		}
		evt.Message.Payload = typed
		if ver > 0 {
			evt.SchemaVersion = ver
		}
	}
	return evt, nil
}
