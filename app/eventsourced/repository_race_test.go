package eventsourced

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"gochen/errors"
	"gochen/eventing/store"
)

// TestEventSourcedRepository_ConcurrentSaveDifferentAggregates 验证 EventSourcedRepository ConcurrentSaveDifferentAggregates。
func TestEventSourcedRepository_ConcurrentSaveDifferentAggregates(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	eventStore := store.NewMemoryEventStore()
	reg, upgraders := newTestRegistryAndUpgraders()
	require.NoError(t, reg.Register("ValueSet", func() any { return &valueSetEvent{} }))

	storeAdapter, err := NewDomainEventStore(DomainEventStoreOptions[*testAggregate, int64]{
		AggregateType:    "TestAggregate",
		EventStore:       eventStore,
		EventRegistry:    reg,
		UpgraderRegistry: upgraders,
	})
	require.NoError(t, err)

	repo, err := newTestEventSourcedRepository[*testAggregate]("TestAggregate", &testAggregate{}, AdaptAggregateFactory(newTestAggregate), storeAdapter)
	require.NoError(t, err)

	const aggregates = 16
	const eventsPerAgg = 8

	var wg sync.WaitGroup
	wg.Add(aggregates)

	for id := 1; id <= aggregates; id++ {
		aggID := int64(id)
		go func() {
			defer wg.Done()

			agg := newTestAggregate(aggID)
			for i := 0; i < eventsPerAgg; i++ {
				err := agg.ApplyAndRecord(&valueSetEvent{V: i + 1})
				require.NoError(t, err)
			}

			err := repo.Save(ctx, agg)
			require.NoError(t, err)
		}()
	}

	wg.Wait()

	for id := 1; id <= aggregates; id++ {
		aggID := int64(id)
		loaded, err := repo.Get(ctx, aggID)
		require.NoError(t, err)
		require.Equal(t, uint64(eventsPerAgg), loaded.GetVersion())
	}
}

// TestEventSourcedRepository_ConcurrentSaveSameAggregate_Conflict 验证 EventSourcedRepository ConcurrentSaveSameAggregate Conflict。
func TestEventSourcedRepository_ConcurrentSaveSameAggregate_Conflict(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	eventStore := store.NewMemoryEventStore()
	reg, upgraders := newTestRegistryAndUpgraders()
	require.NoError(t, reg.Register("ValueSet", func() any { return &valueSetEvent{} }))

	storeAdapter, err := NewDomainEventStore(DomainEventStoreOptions[*testAggregate, int64]{
		AggregateType:    "TestAggregate",
		EventStore:       eventStore,
		EventRegistry:    reg,
		UpgraderRegistry: upgraders,
	})
	require.NoError(t, err)

	repo, err := newTestEventSourcedRepository[*testAggregate]("TestAggregate", &testAggregate{}, AdaptAggregateFactory(newTestAggregate), storeAdapter)
	require.NoError(t, err)

	const aggID = int64(999)
	const concurrentWriters = 10

	var mu sync.Mutex
	successCount := 0
	conflictCount := 0

	var wg sync.WaitGroup
	wg.Add(concurrentWriters)

	for i := 0; i < concurrentWriters; i++ {
		writerID := i + 1
		go func() {
			defer wg.Done()

			// 每个 goroutine 独立创建聚合实例并尝试保存
			agg := newTestAggregate(aggID)
			err := agg.ApplyAndRecord(&valueSetEvent{V: writerID})
			require.NoError(t, err)

			err = repo.Save(ctx, agg)

			mu.Lock()
			defer mu.Unlock()

			if err == nil {
				successCount++
			} else {
				// 验证错误是并发冲突
				require.True(t, errors.Is(err, errors.Concurrency), "错误应该是 ConcurrencyError")
				conflictCount++
			}
		}()
	}

	wg.Wait()

	// 断言：只有一个成功，其余全部冲突
	require.Equal(t, 1, successCount, "应该只有一个写入成功")
	require.Equal(t, concurrentWriters-1, conflictCount, "其余写入应触发版本冲突")

	// 验证最终状态：聚合版本为 1
	loaded, err := repo.Get(ctx, aggID)
	require.NoError(t, err)
	require.Equal(t, uint64(1), loaded.GetVersion())
}

// TestEventSourcedRepository_ConcurrentReadWrite 验证 EventSourcedRepository ConcurrentReadWrite。
func TestEventSourcedRepository_ConcurrentReadWrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	eventStore := store.NewMemoryEventStore()
	reg, upgraders := newTestRegistryAndUpgraders()
	require.NoError(t, reg.Register("ValueSet", func() any { return &valueSetEvent{} }))

	storeAdapter, err := NewDomainEventStore(DomainEventStoreOptions[*testAggregate, int64]{
		AggregateType:    "TestAggregate",
		EventStore:       eventStore,
		EventRegistry:    reg,
		UpgraderRegistry: upgraders,
	})
	require.NoError(t, err)

	repo, err := newTestEventSourcedRepository[*testAggregate]("TestAggregate", &testAggregate{}, AdaptAggregateFactory(newTestAggregate), storeAdapter)
	require.NoError(t, err)

	const aggID = int64(888)
	const updates = 20
	const readers = 5

	var wg sync.WaitGroup

	// Writer: 串行更新聚合（先创建，再逐步更新）
	wg.Add(1)
	go func() {
		defer wg.Done()

		// 首次创建并保存聚合
		agg := newTestAggregate(aggID)
		err := agg.ApplyAndRecord(&valueSetEvent{V: 1})
		require.NoError(t, err)
		err = repo.Save(ctx, agg)
		require.NoError(t, err)

		// 后续更新
		for i := 2; i <= updates; i++ {
			agg, err := repo.Get(ctx, aggID)
			require.NoError(t, err)
			require.NotNil(t, agg)

			err = agg.ApplyAndRecord(&valueSetEvent{V: i})
			require.NoError(t, err)

			err = repo.Save(ctx, agg)
			require.NoError(t, err)
		}
	}()

	// Readers: 并发读取
	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for i := 0; i < updates; i++ {
				loaded, err := repo.Get(ctx, aggID)
				// 允许读到 nil（聚合尚未创建）或 err（聚合不存在）
				if err == nil && loaded != nil && loaded.GetVersion() > 0 {
					// 验证一致性：版本号与 Value 应合理
					require.GreaterOrEqual(t, loaded.Value, 1, "Value 应已被设置")
					require.LessOrEqual(t, loaded.Value, updates, "Value 不应超过最大更新次数")
				}
			}
		}()
	}

	wg.Wait()

	// 最终验证
	final, err := repo.Get(ctx, aggID)
	require.NoError(t, err)
	require.Equal(t, uint64(updates), final.GetVersion())
	require.Equal(t, updates, final.Value)
}
