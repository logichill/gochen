package eventing

// ToStorable 将值类型的 Event[ID] 切片转换为 IStorableEvent[ID] 切片（指针形式）。
//
// 说明：
//   - Event 的部分方法为指针接收者，接口要求使用 *Event[ID] 实例；
//   - 为避免多个元素指向同一地址，这里对切片元素逐个拷贝。
func ToStorable[ID comparable](events []Event[ID]) []IStorableEvent[ID] {
	if len(events) == 0 {
		return nil
	}
	res := make([]IStorableEvent[ID], len(events))
	for i := range events {
		e := events[i] // 创建副本，避免取同一地址
		res[i] = &e
	}
	return res
}
