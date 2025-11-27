package entity

import "gochen/eventing"

// AggregateRoot 包装 EventSourcedAggregate[int64],提供简化的 API
// 注意: gochen 框架的 Event Sourcing 基于 int64 ID
type AggregateRoot struct {
	*EventSourcedAggregate[int64]
}

// NewAggregateRoot 创建 int64 ID 的事件溯源聚合根
func NewAggregateRoot() AggregateRoot {
	return AggregateRoot{
		EventSourcedAggregate: NewEventSourcedAggregate[int64](0, ""),
	}
}

// RecordEvent 记录领域事件 (ApplyAndRecord 的简化命名)
func (a *AggregateRoot) RecordEvent(evt eventing.IEvent) error {
	return a.ApplyAndRecord(evt)
}
