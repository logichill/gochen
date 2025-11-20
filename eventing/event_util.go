package eventing

// ToStorable 将值类型的 Event 切片转换为 IStorableEvent 切片（指针形式）
// 说明：Event 的部分方法为指针接收者，接口要求使用 *Event 实例。
func ToStorable(events []Event) []IStorableEvent {
	if len(events) == 0 {
		return nil
	}
	res := make([]IStorableEvent, len(events))
	for i := range events {
		e := events[i] // 创建副本，避免取同一地址
		res[i] = &e
	}
	return res
}
