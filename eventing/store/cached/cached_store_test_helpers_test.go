package cached

import "gochen/eventing"

// makeTestEvent aggregateID：对象/实体标识。
//
// 参数：
// - eventType：事件类型
// - version：版本号（类型：uint64）
//
// 返回：
// - result：测试返回值（类型：eventing.Event[int64]）
func makeTestEvent(aggregateID int64, eventType string, version uint64) eventing.Event[int64] {
	return *eventing.NewEvent[int64](aggregateID, "TestAggregate", eventType, version, nil)
}

// toStorableEvents events：事件列表（待追加/发布）（类型：[]eventing.Event[int64]）。
//
// 参数：
//
// 返回：
// - result：列表结果（元素类型：eventing.IStorableEvent[int64]）
func toStorableEvents(events []eventing.Event[int64]) []eventing.IStorableEvent[int64] {
	storable := make([]eventing.IStorableEvent[int64], len(events))
	for i := range events {
		storable[i] = &events[i]
	}
	return storable
}
