package eventsourced

import (
	"testing"

	esstore "gochen/app/eventsourced/eventstore"
	deventsourced "gochen/domain/eventsourced"
	"gochen/eventing/store"
	"gochen/testkit/require"
)

// 测试用领域事件与聚合
type setEvent struct{ V int }

// EventType 返回事件类型标识。
func (e *setEvent) EventType() string { return "Set" }

type serviceAggregate struct {
	*deventsourced.EventSourcedAggregate[int64]
	Value int
}

// newServiceAggregate id：对象/实体标识。
func newServiceAggregate(id int64) *serviceAggregate {
	a := &serviceAggregate{}
	agg, err := deventsourced.InitAggregate[int64](testMetadataRegistry, a, id, "ServiceAggregate")
	if err != nil {
		panic(err)
	}
	a.EventSourcedAggregate = agg
	return a
}

// ApplySetEvent 应用 setEvent。
func (a *serviceAggregate) ApplySetEvent(evt *setEvent) {
	a.Value = evt.V
}

type setCommand struct {
	ID int64
	V  int
}

// AggregateID 返回聚合标识。
func (c *setCommand) AggregateID() int64 { return c.ID }

func newCommandServiceRepository(t *testing.T) *EventSourcedRepository[*serviceAggregate, int64] {
	t.Helper()
	eventStore := store.NewMemoryEventStore[int64]()
	reg, upgraders := newTestRegistryAndUpgraders()
	require.NoError(t, reg.Register("Set", func() any { return &setEvent{} }))
	adapter, err := esstore.NewDomainEventStore(esstore.DomainEventStoreOptions[int64]{
		PublishMode:      esstore.PublishModePersistOnly,
		AggregateType:    "ServiceAggregate",
		EventIDGenerator: testEventIDGenerator(),
		EventStore:       eventStore,
		EventRegistry:    reg,
		UpgraderRegistry: upgraders,
	})
	require.NoError(t, err)

	repo, err := newTestEventSourcedRepository[*serviceAggregate, int64]("ServiceAggregate", &serviceAggregate{}, AdaptAggregateFactory(newServiceAggregate), adapter)
	require.NoError(t, err)
	return repo
}
