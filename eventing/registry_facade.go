package eventing

import "gochen/eventing/registry"

// GetEventSchemaVersion 返回事件类型的最新模式版本
//
// 语义：
//   - 对于已注册的事件类型，返回当前注册的 schema 版本号；
//   - 未注册的事件类型返回 0，调用方应据此决定默认行为或拒绝处理。
// 并发安全：
//   - registry 包内部保证注册表的并发安全，本函数为只读访问。
func GetEventSchemaVersion(eventType string) int {
	return registry.GetEventSchemaVersion(eventType)
}
