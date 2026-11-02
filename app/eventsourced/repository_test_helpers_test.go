package eventsourced

import (
	deventsourced "gochen/domain/eventsourced"
)

// 测试用领域事件
type valueSetEvent struct{ V int }

// EventType 返回事件类型标识。
//
// 返回：
// - result：文本结果
func (e *valueSetEvent) EventType() string { return "ValueSet" }

// 测试用聚合
type testAggregate struct {
	*deventsourced.EventSourcedAggregate[int64]
	Value          int
	Department     *int64 `dim:"department"`
	Classification bool   `dim:"classification"`
}

type dimensionsSetEvent struct {
	Department     *int64
	Classification bool
}

func (*dimensionsSetEvent) EventType() string { return "DimensionsSet" }
func (a *testAggregate) ApplyDimensionsSetEvent(evt *dimensionsSetEvent) {
	a.Department = nil
	if evt.Department != nil {
		value := *evt.Department
		a.Department = &value
	}
	a.Classification = evt.Classification
}

// newTestAggregate id：对象/实体标识。
//
// 参数：
//
// 返回：
// - result：返回的实例（类型：*testAggregate）
func newTestAggregate(id int64) *testAggregate {
	a := &testAggregate{}
	agg, err := deventsourced.InitAggregate[int64](testMetadataRegistry, a, id, "TestAggregate")
	if err != nil {
		panic(err)
	}
	a.EventSourcedAggregate = agg
	return a
}

// ApplyValueSetEvent 应用 valueSetEvent。
func (a *testAggregate) ApplyValueSetEvent(evt *valueSetEvent) {
	a.Value = evt.V
}
