package eventstore_test

import (
	deventsourced "gochen/domain/eventsourced"
	"gochen/eventing"
	"gochen/eventing/registry"
	"gochen/eventing/upcast"
	"gochen/gen"
)

type testStringGeneratorFunc func() (string, error)

func (f testStringGeneratorFunc) Next() (string, error) { return f() }

func testEventIDGenerator() gen.IGenerator[string] {
	return gen.NewUUIDGenerator()
}

func newTestEvent[ID comparable](aggregateID ID, aggregateType, eventType string, version uint64, data any, schemaVersion ...int) *eventing.Event[ID] {
	event, err := eventing.NewEvent(testEventIDGenerator(), aggregateID, aggregateType, eventType, version, data, schemaVersion...)
	if err != nil {
		panic(err)
	}
	return event
}

func newTestRegistryAndUpgraders() (*registry.Registry, *upcast.UpgraderRegistry) {
	return registry.NewRegistry(), upcast.NewUpgraderRegistry()
}

type valueSetEvent struct{ V int }

// EventType 返回事件类型标识。
//
// 返回：
// - result：文本结果
func (e *valueSetEvent) EventType() string { return "ValueSet" }

// 测试用聚合
type testAggregate struct {
	*deventsourced.EventSourcedAggregate[int64]
	Value int
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

var testMetadataRegistry = func() *deventsourced.MetadataRegistry {
	registry := deventsourced.NewMetadataRegistry()
	for _, entry := range []struct {
		sample any
		name   string
	}{
		{&testAggregate{}, "TestAggregate"}, {&snapshotAggregate{}, "SnapAggregate"},
		{&autoAgg{}, "AutoAgg"}, {&benchAggregate{}, "BenchAggregate"},
	} {
		if _, err := registry.Register(entry.sample, entry.name); err != nil {
			panic(err)
		}
	}
	return registry
}()
