package store

import (
	"context"
	"testing"

	"gochen/eventing"
)

// BenchmarkMemoryEventStore_AppendEvents 测试追加事件的性能
func BenchmarkMemoryEventStore_AppendEvents(b *testing.B) {
	ctx := context.Background()

	b.Run("Single Event", func(b *testing.B) {
		store := NewMemoryEventStore()
		// 构造简单测试事件，主要关注 AppendEvents 的存储开销
		createEvent := func(aggregateID int64) eventing.IStorableEvent[int64] {
			return eventing.NewEvent(
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
			if err := store.AppendEvents(ctx, aggregateID, events, 0); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("10 Events", func(b *testing.B) {
		store := NewMemoryEventStore()
		// 构造简单测试事件，主要关注 AppendEvents 的存储开销
		createEvents := func(aggregateID int64, count int) []eventing.IStorableEvent[int64] {
			events := make([]eventing.IStorableEvent[int64], count)
			for i := 0; i < count; i++ {
				events[i] = eventing.NewEvent(
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
			if err := store.AppendEvents(ctx, aggregateID, events, 0); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("100 Events", func(b *testing.B) {
		store := NewMemoryEventStore()
		createEvents := func(aggregateID int64, count int) []eventing.IStorableEvent[int64] {
			events := make([]eventing.IStorableEvent[int64], count)
			for i := 0; i < count; i++ {
				events[i] = eventing.NewEvent(
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
			if err := store.AppendEvents(ctx, aggregateID, events, 0); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkMemoryEventStore_LoadEvents 测试加载事件的性能
func BenchmarkMemoryEventStore_LoadEvents(b *testing.B) {
	ctx := context.Background()

	// 辅助函数：创建并插入事件
	createAndAppendEvents := func(store *MemoryEventStore, aggregateID int64, count int) {
		events := make([]eventing.IStorableEvent[int64], count)
		for i := 0; i < count; i++ {
			events[i] = eventing.NewEvent(
				aggregateID,
				"TestAggregate",
				"TestEvent",
				uint64(i+1),
				map[string]interface{}{"data": "test"},
			)
		}
		if err := store.AppendEvents(ctx, aggregateID, events, 0); err != nil {
			b.Fatal(err)
		}
	}

	b.Run("Load 10 Events", func(b *testing.B) {
		store := NewMemoryEventStore()
		aggregateID := int64(1)
		createAndAppendEvents(store, aggregateID, 10)
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, err := store.LoadEvents(ctx, aggregateID, 0)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("Load 100 Events", func(b *testing.B) {
		store := NewMemoryEventStore()
		aggregateID := int64(2)
		createAndAppendEvents(store, aggregateID, 100)
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, err := store.LoadEvents(ctx, aggregateID, 0)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("Load 1000 Events", func(b *testing.B) {
		store := NewMemoryEventStore()
		aggregateID := int64(3)
		createAndAppendEvents(store, aggregateID, 1000)
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, err := store.LoadEvents(ctx, aggregateID, 0)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("Load After Version (10/100)", func(b *testing.B) {
		store := NewMemoryEventStore()
		aggregateID := int64(4)
		createAndAppendEvents(store, aggregateID, 100)
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, err := store.LoadEvents(ctx, aggregateID, 10)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("Load After Version (99/100)", func(b *testing.B) {
		store := NewMemoryEventStore()
		aggregateID := int64(5)
		createAndAppendEvents(store, aggregateID, 100)
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, err := store.LoadEvents(ctx, aggregateID, 99)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkMemoryEventStore_Concurrent 测试并发性能
func BenchmarkMemoryEventStore_Concurrent(b *testing.B) {
	store := NewMemoryEventStore()
	ctx := context.Background()

	createEvent := func(aggregateID int64, version uint64) eventing.IStorableEvent[int64] {
		return eventing.NewEvent(
			aggregateID,
			"TestAggregate",
			"TestEvent",
			version,
			map[string]interface{}{"data": "test"},
		)
	}

	b.Run("ConcurrentAppend", func(b *testing.B) {
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			i := int64(0)
			for pb.Next() {
				i++
				aggregateID := i % 100 // 100 个不同的聚合
				events := []eventing.IStorableEvent[int64]{createEvent(aggregateID, 1)}
				_ = store.AppendEvents(ctx, aggregateID+1000000, events, 0)
			}
		})
	})

	b.Run("ConcurrentLoad", func(b *testing.B) {
		// 预先插入数据
		for i := int64(0); i < 100; i++ {
			events := []eventing.IStorableEvent[int64]{createEvent(i, 1)}
			_ = store.AppendEvents(ctx, i+2000000, events, 0)
		}

		b.ResetTimer()
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			i := int64(0)
			for pb.Next() {
				i++
				aggregateID := (i % 100) + 2000000
				_, _ = store.LoadEvents(ctx, aggregateID, 0)
			}
		})
	})

	b.Run("ConcurrentMixed", func(b *testing.B) {
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			i := int64(0)
			for pb.Next() {
				i++
				aggregateID := (i % 100) + 3000000
				if i%2 == 0 {
					// 写
					events := []eventing.IStorableEvent[int64]{createEvent(aggregateID, 1)}
					_ = store.AppendEvents(ctx, aggregateID, events, 0)
				} else {
					// 读
					_, _ = store.LoadEvents(ctx, aggregateID, 0)
				}
			}
		})
	})
}
