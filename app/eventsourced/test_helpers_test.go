package eventsourced

import (
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
