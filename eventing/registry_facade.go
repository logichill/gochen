package eventing

import "gochen/eventing/registry"

// GetEventSchemaVersion 返回事件类型的最新模式版本
func GetEventSchemaVersion(eventType string) int {
	return registry.GetEventSchemaVersion(eventType)
}
