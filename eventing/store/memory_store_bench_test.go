package store

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"gochen/eventing"
	"gochen/eventing/internal/testutil"
)

// BenchmarkMemoryEventStore_AppendEvents 用于评估 MemoryEventStore AppendEvents 的性能。
func BenchmarkMemoryEventStore_AppendEvents(b *testing.B) {
	ctx := context.Background()

	b.Run("Single Event", func(b *testing.B) {
		store := NewMemoryEventStore[int64]()
		// 构造简单测试事件，主要关注 AppendEvents 的存储开销
		createEvent := func(aggregateID int64) eventing.IStorableEvent[int64] {
			return testutil.NewEvent(
				aggregateID,
				"TestAggregate",
				"TestEvent",
				1,
				map[string]interface{}{"data": "test"},
			)
		}

		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			aggregateID := int64(i + 1000)
			events := []eventing.IStorableEvent[int64]{createEvent(aggregateID)}
			if err := store.AppendEvents(ctx, "TestAggregate", aggregateID, events, 0); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("10 Events", func(b *testing.B) {
		store := NewMemoryEventStore[int64]()
		// 构造简单测试事件，主要关注 AppendEvents 的存储开销
		createEvents := func(aggregateID int64, count int) []eventing.IStorableEvent[int64] {
			events := make([]eventing.IStorableEvent[int64], count)
			for i := 0; i < count; i++ {
				events[i] = testutil.NewEvent(
					aggregateID,
					"TestAggregate",
					"TestEvent",
					uint64(i+1),
					map[string]interface{}{"data": "test"},
				)
			}
			return events
		}

		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			aggregateID := int64(i + 10000)
			events := createEvents(aggregateID, 10)
			if err := store.AppendEvents(ctx, "TestAggregate", aggregateID, events, 0); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("100 Events", func(b *testing.B) {
		store := NewMemoryEventStore[int64]()
		createEvents := func(aggregateID int64, count int) []eventing.IStorableEvent[int64] {
			events := make([]eventing.IStorableEvent[int64], count)
			for i := 0; i < count; i++ {
				events[i] = testutil.NewEvent(
					aggregateID,
					"TestAggregate",
					"TestEvent",
					uint64(i+1),
					map[string]interface{}{"data": "test"},
				)
			}
			return events
		}

		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			aggregateID := int64(i + 100000)
			events := createEvents(aggregateID, 100)
			if err := store.AppendEvents(ctx, "TestAggregate", aggregateID, events, 0); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkMemoryEventStore_InsertOutOfOrderGlobalEvent(b *testing.B) {
	for _, historySize := range []int{1_000, 10_000} {
		b.Run(fmt.Sprintf("History_%d", historySize), func(b *testing.B) {
			baseTime := time.Unix(1_700_000_000, 0)
			history := make([]eventing.Event[int64], historySize)
			for i := range history {
				evt := testutil.NewEvent[int64](int64(i+1), "TestAggregate", "TestEvent", 1, nil)
				evt.ID = fmt.Sprintf("history-%08d", i)
				evt.Timestamp = baseTime.Add(time.Duration(i) * time.Second)
				history[i] = *evt
			}

			store := NewMemoryEventStore[int64]()
			incoming := testutil.NewEvent[int64](int64(historySize+1), "TestAggregate", "TestEvent", 1, nil)
			incoming.ID = "incoming"
			incoming.Timestamp = baseTime.Add(time.Duration(historySize/2) * time.Second)

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				store.globalEvents = append(store.globalEvents[:0], history...)
				b.StartTimer()
				store.insertGlobalEventsUnsafe([]eventing.Event[int64]{*incoming})
			}
		})
	}
}

// BenchmarkMemoryEventStore_LoadEvents 用于评估 MemoryEventStore LoadEvents 的性能。
func BenchmarkMemoryEventStore_LoadEvents(b *testing.B) {
	ctx := context.Background()

	// 辅助函数：创建并插入事件
	createAndAppendEvents := func(store *MemoryEventStore[int64], aggregateID int64, count int) {
		events := make([]eventing.IStorableEvent[int64], count)
		for i := 0; i < count; i++ {
			events[i] = testutil.NewEvent(
				aggregateID,
				"TestAggregate",
				"TestEvent",
				uint64(i+1),
				map[string]interface{}{"data": "test"},
			)
		}
		if err := store.AppendEvents(ctx, "TestAggregate", aggregateID, events, 0); err != nil {
			b.Fatal(err)
		}
	}

	b.Run("Load 10 Events", func(b *testing.B) {
		store := NewMemoryEventStore[int64]()
		aggregateID := int64(1)
		createAndAppendEvents(store, aggregateID, 10)
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, err := store.LoadEvents(ctx, "TestAggregate", aggregateID, 0)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("Load 100 Events", func(b *testing.B) {
		store := NewMemoryEventStore[int64]()
		aggregateID := int64(2)
		createAndAppendEvents(store, aggregateID, 100)
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, err := store.LoadEvents(ctx, "TestAggregate", aggregateID, 0)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("Load 1000 Events", func(b *testing.B) {
		store := NewMemoryEventStore[int64]()
		aggregateID := int64(3)
		createAndAppendEvents(store, aggregateID, 1000)
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, err := store.LoadEvents(ctx, "TestAggregate", aggregateID, 0)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("Load After Version (10/100)", func(b *testing.B) {
		store := NewMemoryEventStore[int64]()
		aggregateID := int64(4)
		createAndAppendEvents(store, aggregateID, 100)
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, err := store.LoadEvents(ctx, "TestAggregate", aggregateID, 10)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("Load After Version (99/100)", func(b *testing.B) {
		store := NewMemoryEventStore[int64]()
		aggregateID := int64(5)
		createAndAppendEvents(store, aggregateID, 100)
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, err := store.LoadEvents(ctx, "TestAggregate", aggregateID, 99)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkMemoryEventStore_Concurrent 用于评估 MemoryEventStore Concurrent 的性能。
func BenchmarkMemoryEventStore_Concurrent(b *testing.B) {
	ctx := context.Background()

	createEvent := func(aggregateID int64, version uint64) eventing.IStorableEvent[int64] {
		return testutil.NewEvent(
			aggregateID,
			"TestAggregate",
			"TestEvent",
			version,
			map[string]interface{}{"data": "test"},
		)
	}

	b.Run("ConcurrentAppend", func(b *testing.B) {
		store := NewMemoryEventStore[int64]()
		var nextID atomic.Int64
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				aggregateID := int64(1_000_000) + nextID.Add(1)
				events := []eventing.IStorableEvent[int64]{createEvent(aggregateID, 1)}
				if err := store.AppendEvents(ctx, "TestAggregate", aggregateID, events, 0); err != nil {
					b.Error(err)
					return
				}
			}
		})
	})

	b.Run("ConcurrentLoad", func(b *testing.B) {
		store := NewMemoryEventStore[int64]()
		// 预先插入数据
		for i := int64(0); i < 100; i++ {
			aggregateID := i + 2_000_000
			events := []eventing.IStorableEvent[int64]{createEvent(aggregateID, 1)}
			if err := store.AppendEvents(ctx, "TestAggregate", aggregateID, events, 0); err != nil {
				b.Fatal(err)
			}
		}

		b.ResetTimer()
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			i := int64(0)
			for pb.Next() {
				i++
				aggregateID := (i % 100) + 2000000
				if _, err := store.LoadEvents(ctx, "TestAggregate", aggregateID, 0); err != nil {
					b.Error(err)
					return
				}
			}
		})
	})

	b.Run("ConcurrentMixed", func(b *testing.B) {
		store := NewMemoryEventStore[int64]()
		for i := int64(0); i < 100; i++ {
			aggregateID := i + 3_000_000
			if err := store.AppendEvents(ctx, "TestAggregate", aggregateID, []eventing.IStorableEvent[int64]{createEvent(aggregateID, 1)}, 0); err != nil {
				b.Fatal(err)
			}
		}
		var operation atomic.Int64
		b.ResetTimer()
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				i := operation.Add(1)
				if i%2 == 0 {
					aggregateID := int64(4_000_000) + i
					events := []eventing.IStorableEvent[int64]{createEvent(aggregateID, 1)}
					if err := store.AppendEvents(ctx, "TestAggregate", aggregateID, events, 0); err != nil {
						b.Error(err)
						return
					}
				} else {
					aggregateID := (i % 100) + 3_000_000
					if _, err := store.LoadEvents(ctx, "TestAggregate", aggregateID, 0); err != nil {
						b.Error(err)
						return
					}
				}
			}
		})
	})
}
