package eventstore_test

import (
	esapp "gochen/app/eventsourced"
	deventsourced "gochen/domain/eventsourced"
)

func newTestEventSourcedRepository[T deventsourced.IEventSourcedAggregate[ID], ID comparable](
	aggregateType string,
	sample T,
	factory func(id ID) (T, error),
	store deventsourced.IDomainEventStore[ID],
) (*esapp.EventSourcedRepository[T, ID], error) {
	return esapp.NewEventSourcedRepository(esapp.RepositoryOptions[T, ID]{
		AggregateType:    aggregateType,
		Sample:           sample,
		Factory:          factory,
		Store:            store,
		MetadataRegistry: testMetadataRegistry,
	})
}
