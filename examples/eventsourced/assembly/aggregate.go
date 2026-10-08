package main

import (
	esapp "gochen/app/eventsourced"
	esstore "gochen/app/eventsourced/eventstore"
	es "gochen/domain/eventsourced"
	"gochen/eventing/registry"
	"gochen/eventing/store"
	"gochen/eventing/upcast"
	"gochen/gen"
	"gochen/observe/logging"
)

type balanceChanged struct{ Delta int }

func (*balanceChanged) EventType() string { return "BalanceChanged" }

type account struct {
	*es.EventSourcedAggregate[int64]
	Balance int
}

func (a *account) ApplyBalanceChanged(e *balanceChanged) { a.Balance += e.Delta }
func (a *account) SnapshotData() any                     { return struct{ Balance int }{a.Balance} }

type assembly struct {
	metadata  *es.MetadataRegistry
	events    *store.MemoryEventStore[int64]
	registry  *registry.Registry
	upgraders *upcast.UpgraderRegistry
}

func newAssembly() *assembly {
	m := es.NewMetadataRegistry()
	_, err := m.Register(&account{}, "Account")
	must(err)
	r := registry.NewRegistry()
	must(r.Register("BalanceChanged", func() any { return &balanceChanged{} }))
	return &assembly{m, store.NewMemoryEventStore[int64](), r, upcast.NewUpgraderRegistry()}
}
func (s *assembly) aggregate(id int64) *account {
	a := &account{}
	base, err := es.InitAggregate(s.metadata, a, id, "Account")
	must(err)
	a.EventSourcedAggregate = base
	return a
}
func (s *assembly) options(mode esstore.PublishMode) esstore.DomainEventStoreOptions[int64] {
	return esstore.DomainEventStoreOptions[int64]{PublishMode: mode, AggregateType: "Account", EventIDGenerator: gen.NewUUIDGenerator(), EventStore: s.events, EventRegistry: s.registry, UpgraderRegistry: s.upgraders, Logger: logging.NewNoopLogger()}
}
func (s *assembly) repositoryOptions(storage es.IDomainEventStore[int64]) esapp.RepositoryOptions[*account, int64] {
	return esapp.RepositoryOptions[*account, int64]{AggregateType: "Account", Sample: &account{}, Factory: func(id int64) (*account, error) { return s.aggregate(id), nil }, Store: storage, MetadataRegistry: s.metadata}
}
