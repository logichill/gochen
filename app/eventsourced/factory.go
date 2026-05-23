package eventsourced

import "reflect"

// AdaptAggregateFactory 把只返回聚合实例的工厂函数适配为 `(T, error)` 形式。
//
// 这允许调用方继续使用简单构造函数，同时满足仓储/应用层对统一工厂签名的要求。
func AdaptAggregateFactory[T any, ID comparable, F ~func(id ID) T](factory F) func(id ID) (T, error) {
	if value := reflect.ValueOf(factory); value.Kind() == reflect.Func && value.IsNil() {
		return nil
	}
	return func(id ID) (T, error) {
		return factory(id), nil
	}
}
