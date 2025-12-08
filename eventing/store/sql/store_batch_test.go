package sql

import (
	"context"
	"testing"

	"gochen/data/db"
	"gochen/eventing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSQLEventStore_LoadEventsBatch(t *testing.T) {
	store := setupTestStoreForBatch(t)

	ctx := context.Background()

	// 准备测试数据：3个聚合，每个2个事件
	aggregateIDs := []int64{1, 2, 3}
	for _, id := range aggregateIDs {
		events := []eventing.IStorableEvent[int64]{
			eventing.NewEvent(id, "TestAggregate", "Event1", 1, map[string]interface{}{"data": "test1"}),
			eventing.NewEvent(id, "TestAggregate", "Event2", 2, map[string]interface{}{"data": "test2"}),
		}
		err := store.AppendEvents(ctx, id, events, 0)
		require.NoError(t, err)
	}

	t.Run("批量加载所有聚合", func(t *testing.T) {
		result, err := store.LoadEventsBatch(ctx, aggregateIDs, 0)
		require.NoError(t, err)
		assert.Len(t, result, 3)

		for _, id := range aggregateIDs {
			events, ok := result[id]
			assert.True(t, ok)
			assert.Len(t, events, 2)
			assert.Equal(t, uint64(1), events[0].GetVersion())
			assert.Equal(t, uint64(2), events[1].GetVersion())
		}
	})

	t.Run("批量加载带版本过滤", func(t *testing.T) {
		result, err := store.LoadEventsBatch(ctx, aggregateIDs, 1)
		require.NoError(t, err)
		assert.Len(t, result, 3)

		for _, id := range aggregateIDs {
			events := result[id]
			assert.Len(t, events, 1)
			assert.Equal(t, uint64(2), events[0].GetVersion())
		}
	})

	t.Run("包含不存在的聚合", func(t *testing.T) {
		ids := []int64{1, 999, 2} // 999 不存在
		result, err := store.LoadEventsBatch(ctx, ids, 0)
		require.NoError(t, err)
		assert.Len(t, result, 3)

		// 存在的聚合有事件
		assert.Len(t, result[1], 2)
		assert.Len(t, result[2], 2)

		// 不存在的聚合返回空切片
		assert.Len(t, result[999], 0)
	})

	t.Run("空聚合列表", func(t *testing.T) {
		result, err := store.LoadEventsBatch(ctx, []int64{}, 0)
		require.NoError(t, err)
		assert.Len(t, result, 0)
	})

	t.Run("去重聚合ID", func(t *testing.T) {
		ids := []int64{1, 2, 1, 2, 1} // 重复ID
		result, err := store.LoadEventsBatch(ctx, ids, 0)
		require.NoError(t, err)
		assert.Len(t, result, 2) // 只返回唯一的聚合
		assert.Len(t, result[1], 2)
		assert.Len(t, result[2], 2)
	})
}

// BenchmarkLoadEventsBatch 对比批量加载和逐个加载的性能
func BenchmarkLoadEventsBatch(b *testing.B) {
	store := setupTestStoreForBatch(b)

	ctx := context.Background()

	// 准备100个聚合，每个10个事件
	aggregateIDs := make([]int64, 100)
	for i := 0; i < 100; i++ {
		id := int64(i + 1)
		aggregateIDs[i] = id

		events := make([]eventing.IStorableEvent[int64], 10)
		for j := 0; j < 10; j++ {
			events[j] = eventing.NewEvent(id, "TestAggregate", "TestEvent", uint64(j+1),
				map[string]interface{}{"index": j})
		}
		_ = store.AppendEvents(ctx, id, events, 0)
	}

	b.Run("逐个加载（旧方式）", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			for _, id := range aggregateIDs {
				_, _ = store.LoadEvents(ctx, id, 0)
			}
		}
	})

	b.Run("批量加载（新方式）", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, _ = store.LoadEventsBatch(ctx, aggregateIDs, 0)
		}
	})
}

// 辅助函数
func setupTestStoreForBatch(t testing.TB) *SQLEventStore {
	t.Helper()
	var database db.IDatabase

	switch v := t.(type) {
	case *testing.T:
		database = setupTestDB(v)
	case *testing.B:
		database = setupTestDB(&testing.T{})
	default:
		panic("unsupported testing type")
	}

	return &SQLEventStore{
		db:        database,
		tableName: "event_store",
	}
}
