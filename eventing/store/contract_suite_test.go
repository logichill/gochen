package store_test

import (
	"testing"

	"gochen/eventing/store"
	"gochen/eventing/store/contracttest"
)

func TestMemoryEventStoreContract(t *testing.T) {
	contracttest.RunEventStore(t, func(*testing.T) store.IEventStore[int64] { return store.NewMemoryEventStore[int64]() })
	contracttest.RunEventStream(t, func(*testing.T) store.IEventStreamStore[int64] { return store.NewMemoryEventStore[int64]() })
	contracttest.RunAtomic(t, func(*testing.T) contracttest.IAtomicBackend { return store.NewMemoryEventStore[int64]() })
}
