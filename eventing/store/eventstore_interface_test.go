package store

import (
	"context"
	"testing"

	"gochen/eventing"
)

func TestIEventStoreRequiresTypedAggregateIdentity(t *testing.T) {
	var _ IEventStore[int64] = minimalEventStore{}
}

type minimalEventStore struct{}

func (minimalEventStore) AppendEvents(context.Context, string, int64, []eventing.IStorableEvent[int64], uint64) error {
	return nil
}

func (minimalEventStore) LoadEvents(context.Context, string, int64, uint64) ([]eventing.Event[int64], error) {
	return nil, nil
}

func (minimalEventStore) HasAggregate(context.Context, string, int64) (bool, error) {
	return false, nil
}

func (minimalEventStore) GetAggregateVersion(context.Context, string, int64) (uint64, error) {
	return 0, nil
}
