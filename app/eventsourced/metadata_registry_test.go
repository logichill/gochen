package eventsourced

import deventsourced "gochen/domain/eventsourced"

var testMetadataRegistry = newAppTestMetadataRegistry()

func newAppTestMetadataRegistry() *deventsourced.MetadataRegistry {
	registry := deventsourced.NewMetadataRegistry()
	mustRegister := func(sample any, aggregateType string) {
		if _, err := registry.Register(sample, aggregateType); err != nil {
			panic(err)
		}
	}
	mustRegister(&testAggregate{}, "TestAggregate")
	mustRegister(&TestAggregate{}, "TestAggregate")
	mustRegister(&autoBindAggregate{}, "AutoBindAggregate")
	mustRegister(&outboxAggregate{}, "OutboxAggregate")
	mustRegister(&serviceAggregate{}, "ServiceAggregate")
	mustRegister(&snapshotAggregate{}, "SnapAggregate")
	mustRegister(&autoAgg{}, "AutoAgg")
	mustRegister(&benchAggregate{}, "BenchAggregate")
	return registry
}

func newTestEventSourcedRepository[T deventsourced.IEventSourcedAggregate[ID], ID comparable](
	aggregateType string,
	sample T,
	factory func(id ID) (T, error),
	store deventsourced.IDomainEventStore[ID],
) (*EventSourcedRepository[T, ID], error) {
	return NewEventSourcedRepository(RepositoryOptions[T, ID]{
		AggregateType:    aggregateType,
		Sample:           sample,
		Factory:          factory,
		Store:            store,
		MetadataRegistry: testMetadataRegistry,
	})
}
