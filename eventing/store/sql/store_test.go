package sql

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gochen/eventing"
	estore "gochen/eventing/store"
	"gochen/storage/database"
	basicdb "gochen/storage/database/basic"
)

// 测试辅助：创建内存数据库并初始化表
func setupTestDB(t *testing.T) database.IDatabase {
	db, err := basicdb.New(database.DBConfig{Driver: "sqlite", Database: ":memory:"})
	require.NoError(t, err)
	ctx := context.Background()
	_, err = db.Exec(ctx, `
        CREATE TABLE domain_events (
            id TEXT PRIMARY KEY,
            type TEXT NOT NULL,
            aggregate_id INTEGER NOT NULL,
            aggregate_type TEXT NOT NULL,
            version INTEGER NOT NULL,
            schema_version INTEGER NOT NULL,
            timestamp DATETIME NOT NULL,
            payload TEXT NOT NULL,
            metadata TEXT NOT NULL,
            UNIQUE(aggregate_id, aggregate_type, version)
        );
    `)
	require.NoError(t, err)
	return db
}

func makeEvent(aggregateID int64, aggregateType, id string, version uint64, payload map[string]interface{}) eventing.Event {
	if payload == nil {
		payload = make(map[string]interface{})
	}
	e := eventing.NewEvent(aggregateID, aggregateType, "TestEvent", version, payload)
	e.ID = id
	return *e
}

// 辅助函数：将 eventing.Event 切片转换为 IStorableEvent 切片
func toStorableEvents(events []eventing.Event) []eventing.IStorableEvent {
	storable := make([]eventing.IStorableEvent, len(events))
	for i := range events {
		storable[i] = &events[i]
	}
	return storable
}

func TestSQLEventStore_AppendEvents(t *testing.T) {
	db := setupTestDB(t)
	store := NewSQLEventStore(db, "domain_events")

	ctx := context.Background()
	aggregateID := int64(123)

	events := []eventing.Event{
		makeEvent(aggregateID, "TestAggregate", "event-1", 1, map[string]interface{}{"value": 100}),
		makeEvent(aggregateID, "TestAggregate", "event-2", 2, map[string]interface{}{"value": 200}),
	}

	err := store.AppendEvents(ctx, aggregateID, toStorableEvents(events), 0)
	assert.NoError(t, err)

	loaded, err := store.LoadEvents(ctx, aggregateID, 0)
	assert.NoError(t, err)
	assert.Len(t, loaded, 2)
	assert.Equal(t, "event-1", loaded[0].ID)
	assert.Equal(t, "event-2", loaded[1].ID)
}

func TestSQLEventStore_VersionConflict(t *testing.T) {
	db := setupTestDB(t)
	store := NewSQLEventStore(db, "domain_events")

	ctx := context.Background()
	aggregateID := int64(456)

	events1 := []eventing.Event{makeEvent(aggregateID, "TestAggregate", "event-1", 1, nil)}
	err := store.AppendEvents(ctx, aggregateID, toStorableEvents(events1), 0)
	assert.NoError(t, err)

	events2 := []eventing.Event{makeEvent(aggregateID, "TestAggregate", "event-2", 2, nil)}
	err = store.AppendEvents(ctx, aggregateID, toStorableEvents(events2), 0) // 期望版本不匹配
	assert.Error(t, err)
}

func TestSQLEventStore_Idempotency(t *testing.T) {
	db := setupTestDB(t)
	store := NewSQLEventStore(db, "domain_events")

	ctx := context.Background()
	aggregateID := int64(789)

	events := []eventing.Event{makeEvent(aggregateID, "TestAggregate", "event-1", 1, nil)}

	err := store.AppendEvents(ctx, aggregateID, toStorableEvents(events), 0)
	assert.NoError(t, err)

	// 幂等：重复写入同一事件
	_ = store.AppendEvents(ctx, aggregateID, toStorableEvents(events), 0)

	loaded, err := store.LoadEvents(ctx, aggregateID, 0)
	assert.NoError(t, err)
	assert.Len(t, loaded, 1)
}

func TestSQLEventStore_LoadEventsByType(t *testing.T) {
	db := setupTestDB(t)
	store := NewSQLEventStore(db, "domain_events")

	ctx := context.Background()

	events1 := []eventing.Event{makeEvent(1, "TypeA", "event-1", 1, nil)}
	events2 := []eventing.Event{makeEvent(1, "TypeB", "event-2", 1, nil)}

	assert.NoError(t, store.AppendEvents(ctx, 1, toStorableEvents(events1), 0))
	assert.NoError(t, store.AppendEvents(ctx, 1, toStorableEvents(events2), 0))

	loaded, err := store.LoadEventsByType(ctx, "TypeA", 1, 0)
	assert.NoError(t, err)
	assert.Len(t, loaded, 1)
	assert.Equal(t, "TypeA", loaded[0].AggregateType)
}

func TestSQLEventStore_AppendEventsWithDB(t *testing.T) {
	db := setupTestDB(t)
	store := NewSQLEventStore(db, "domain_events")

	ctx := context.Background()
	aggregateID := int64(100)

	// 使用外部事务
	tx, err := db.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback()

	events := []eventing.Event{makeEvent(aggregateID, "TestAggregate", "event-tx-1", 1, nil)}
	err = store.AppendEventsWithDB(ctx, tx, aggregateID, toStorableEvents(events), 0)
	assert.NoError(t, err)

	// 提交事务
	err = tx.Commit()
	assert.NoError(t, err)

	// 验证事件已保存
	loaded, err := store.LoadEvents(ctx, aggregateID, 0)
	assert.NoError(t, err)
	assert.Len(t, loaded, 1)
}

func TestSQLEventStore_HasAggregate(t *testing.T) {
	db := setupTestDB(t)
	store := NewSQLEventStore(db, "domain_events")

	ctx := context.Background()
	aggregateID := int64(200)

	// 聚合不存在
	exists, err := store.HasAggregate(ctx, aggregateID)
	assert.NoError(t, err)
	assert.False(t, exists)

	// 添加事件
	events := []eventing.Event{makeEvent(aggregateID, "TestAggregate", "event-1", 1, nil)}
	err = store.AppendEvents(ctx, aggregateID, toStorableEvents(events), 0)
	require.NoError(t, err)

	// 聚合存在
	exists, err = store.HasAggregate(ctx, aggregateID)
	assert.NoError(t, err)
	assert.True(t, exists)
}

func TestSQLEventStore_GetAggregateVersion(t *testing.T) {
	db := setupTestDB(t)
	store := NewSQLEventStore(db, "domain_events")

	ctx := context.Background()
	aggregateID := int64(300)

	// 新聚合版本为0
	version, err := store.GetAggregateVersion(ctx, aggregateID)
	assert.NoError(t, err)
	assert.Equal(t, uint64(0), version)

	// 添加多个事件
	events := []eventing.Event{
		makeEvent(aggregateID, "TestAggregate", "event-1", 1, nil),
		makeEvent(aggregateID, "TestAggregate", "event-2", 2, nil),
		makeEvent(aggregateID, "TestAggregate", "event-3", 3, nil),
	}
	err = store.AppendEvents(ctx, aggregateID, toStorableEvents(events), 0)
	require.NoError(t, err)

	// 版本应为最后一个事件的版本
	version, err = store.GetAggregateVersion(ctx, aggregateID)
	assert.NoError(t, err)
	assert.Equal(t, uint64(3), version)
}

func TestSQLEventStore_StreamEvents(t *testing.T) {
	db := setupTestDB(t)
	store := NewSQLEventStore(db, "domain_events")

	ctx := context.Background()

	// 添加事件（不同时间）
	events1 := []eventing.Event{makeEvent(1, "TypeA", "event-1", 1, nil)}
	err := store.AppendEvents(ctx, 1, toStorableEvents(events1), 0)
	require.NoError(t, err)

	events2 := []eventing.Event{makeEvent(2, "TypeB", "event-2", 1, nil)}
	err = store.AppendEvents(ctx, 2, toStorableEvents(events2), 0)
	require.NoError(t, err)

	// 从开始时间流式读取
	loaded, err := store.StreamEvents(ctx, events1[0].Timestamp.Add(-1*time.Second))
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, len(loaded), 2)
}

func TestSQLEventStore_GetEventStreamWithCursor(t *testing.T) {
	db := setupTestDB(t)
	store := NewSQLEventStore(db, "domain_events")

	ctx := context.Background()

	// 添加多个事件
	for i := 1; i <= 5; i++ {
		events := []eventing.Event{makeEvent(int64(i), "TestAggregate", fmt.Sprintf("event-%d", i), 1, nil)}
		err := store.AppendEvents(ctx, int64(i), toStorableEvents(events), 0)
		require.NoError(t, err)
	}

	t.Run("基础分页", func(t *testing.T) {
		result, err := store.GetEventStreamWithCursor(ctx, &estore.StreamOptions{
			Limit: 2,
		})
		assert.NoError(t, err)
		assert.Len(t, result.Events, 2)
		assert.True(t, result.HasMore)
		assert.NotEmpty(t, result.NextCursor)
	})

	t.Run("使用游标", func(t *testing.T) {
		// 第一页
		result1, err := store.GetEventStreamWithCursor(ctx, &estore.StreamOptions{
			Limit: 2,
		})
		require.NoError(t, err)

		// 第二页
		result2, err := store.GetEventStreamWithCursor(ctx, &estore.StreamOptions{
			After: result1.NextCursor,
			Limit: 2,
		})
		assert.NoError(t, err)
		assert.Greater(t, len(result2.Events), 0)
	})

	t.Run("按类型过滤", func(t *testing.T) {
		result, err := store.GetEventStreamWithCursor(ctx, &estore.StreamOptions{
			AggregateTypes: []string{"TestAggregate"},
			Limit:          10,
		})
		assert.NoError(t, err)
		assert.Greater(t, len(result.Events), 0)
		for _, evt := range result.Events {
			assert.Equal(t, "TestAggregate", evt.AggregateType)
		}
	})
}

func TestSQLEventStore_LoadEventsAfterVersion(t *testing.T) {
	db := setupTestDB(t)
	store := NewSQLEventStore(db, "domain_events")

	ctx := context.Background()
	aggregateID := int64(400)

	// 添加5个事件
	events := []eventing.Event{
		makeEvent(aggregateID, "TestAggregate", "event-1", 1, nil),
		makeEvent(aggregateID, "TestAggregate", "event-2", 2, nil),
		makeEvent(aggregateID, "TestAggregate", "event-3", 3, nil),
		makeEvent(aggregateID, "TestAggregate", "event-4", 4, nil),
		makeEvent(aggregateID, "TestAggregate", "event-5", 5, nil),
	}
	err := store.AppendEvents(ctx, aggregateID, toStorableEvents(events), 0)
	require.NoError(t, err)

	// 加载版本2之后的事件
	loaded, err := store.LoadEvents(ctx, aggregateID, 2)
	assert.NoError(t, err)
	assert.Len(t, loaded, 3) // 版本3,4,5
	assert.Equal(t, uint64(3), loaded[0].Version)
	assert.Equal(t, uint64(5), loaded[2].Version)
}

func TestSQLEventStore_EmptyEvents(t *testing.T) {
	db := setupTestDB(t)
	store := NewSQLEventStore(db, "domain_events")

	ctx := context.Background()
	aggregateID := int64(500)

	// 添加空事件列表（应该无操作）
	err := store.AppendEvents(ctx, aggregateID, []eventing.IStorableEvent{}, 0)
	assert.NoError(t, err)

	// 验证没有事件
	loaded, err := store.LoadEvents(ctx, aggregateID, 0)
	assert.NoError(t, err)
	assert.Len(t, loaded, 0)
}

func TestSQLEventStore_MixedAggregateTypes(t *testing.T) {
	db := setupTestDB(t)
	store := NewSQLEventStore(db, "domain_events")

	ctx := context.Background()
	aggregateID := int64(600)

	// 尝试在同一批次中添加不同聚合类型的事件（应该失败）
	events := []eventing.Event{
		makeEvent(aggregateID, "TypeA", "event-1", 1, nil),
		makeEvent(aggregateID, "TypeB", "event-2", 2, nil), // 不同类型
	}
	err := store.AppendEvents(ctx, aggregateID, toStorableEvents(events), 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "mixed aggregate types")
}

func TestSQLEventStore_VersionMismatch(t *testing.T) {
	db := setupTestDB(t)
	store := NewSQLEventStore(db, "domain_events")

	ctx := context.Background()
	aggregateID := int64(700)

	// 事件版本与期望版本不匹配
	events := []eventing.Event{
		makeEvent(aggregateID, "TestAggregate", "event-1", 5, nil), // 版本5但期望从1开始
	}
	err := store.AppendEvents(ctx, aggregateID, toStorableEvents(events), 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "version mismatch")
}

func TestSQLEventStore_ComplexPayload(t *testing.T) {
	db := setupTestDB(t)
	store := NewSQLEventStore(db, "domain_events")

	ctx := context.Background()
	aggregateID := int64(800)

	// 复杂载荷
	payload := map[string]interface{}{
		"user": map[string]interface{}{
			"id":   123,
			"name": "Alice",
		},
		"items": []interface{}{
			map[string]interface{}{"id": 1, "qty": 5},
			map[string]interface{}{"id": 2, "qty": 3},
		},
		"total": 99.99,
	}

	events := []eventing.Event{makeEvent(aggregateID, "OrderAggregate", "OrderCreated", 1, payload)}
	err := store.AppendEvents(ctx, aggregateID, toStorableEvents(events), 0)
	assert.NoError(t, err)

	// 验证载荷正确反序列化
	loaded, err := store.LoadEvents(ctx, aggregateID, 0)
	assert.NoError(t, err)
	assert.Len(t, loaded, 1)
	assert.NotNil(t, loaded[0].Payload)

	payloadMap, ok := loaded[0].Payload.(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, 99.99, payloadMap["total"])
}

func TestSQLEventStore_Init(t *testing.T) {
	db := setupTestDB(t)
	store := NewSQLEventStore(db, "domain_events")

	ctx := context.Background()
	err := store.Init(ctx)
	assert.NoError(t, err)
}

func TestSQLEventStore_Getters(t *testing.T) {
	db := setupTestDB(t)
	store := NewSQLEventStore(db, "domain_events")

	assert.Equal(t, "domain_events", store.GetTableName())
	assert.NotNil(t, store.GetDB())
	assert.Equal(t, db, store.GetDB())
}

func TestSQLEventStore_Stream(t *testing.T) {
	db := setupTestDB(t)
	store := NewSQLEventStore(db, "domain_events")

	ctx := context.Background()

	// 添加多个事件
	for i := 1; i <= 5; i++ {
		events := []eventing.Event{makeEvent(int64(i), "TestAggregate", fmt.Sprintf("stream-event-%d", i), 1, nil)}
		err := store.AppendEvents(ctx, int64(i), toStorableEvents(events), 0)
		require.NoError(t, err)
	}

	// 使用 Stream 方法（基于 StreamOptions）
	result, err := store.Stream(ctx, estore.StreamOptions{
		Limit: 3,
	})
	assert.NoError(t, err)
	assert.Len(t, result.Events, 3)
	assert.True(t, result.HasMore)
}

func TestSQLEventStore_GetEventStreamWithCursor_TimeFilters(t *testing.T) {
	db := setupTestDB(t)
	store := NewSQLEventStore(db, "domain_events")

	ctx := context.Background()

	// 添加事件
	now := time.Now()
	for i := 1; i <= 5; i++ {
		evt := makeEvent(int64(i), "TestAggregate", fmt.Sprintf("time-event-%d", i), 1, nil)
		// 设置不同时间
		evt.Timestamp = now.Add(time.Duration(i) * time.Second)
		err := store.AppendEvents(ctx, int64(i), toStorableEvents([]eventing.Event{evt}), 0)
		require.NoError(t, err)
	}

	t.Run("FromTime过滤", func(t *testing.T) {
		result, err := store.GetEventStreamWithCursor(ctx, &estore.StreamOptions{
			FromTime: now.Add(3 * time.Second),
			Limit:    10,
		})
		assert.NoError(t, err)
		assert.GreaterOrEqual(t, len(result.Events), 3) // 事件3,4,5
	})

	t.Run("ToTime过滤", func(t *testing.T) {
		result, err := store.GetEventStreamWithCursor(ctx, &estore.StreamOptions{
			ToTime: now.Add(3 * time.Second),
			Limit:  10,
		})
		assert.NoError(t, err)
		assert.GreaterOrEqual(t, len(result.Events), 3) // 事件1,2,3
	})

	t.Run("时间范围过滤", func(t *testing.T) {
		result, err := store.GetEventStreamWithCursor(ctx, &estore.StreamOptions{
			FromTime: now.Add(2 * time.Second),
			ToTime:   now.Add(4 * time.Second),
			Limit:    10,
		})
		assert.NoError(t, err)
		assert.GreaterOrEqual(t, len(result.Events), 2) // 事件2,3,4
	})
}

func TestSQLEventStore_GetEventStreamWithCursor_TypeFilters(t *testing.T) {
	db := setupTestDB(t)
	store := NewSQLEventStore(db, "domain_events")

	ctx := context.Background()

	// 添加不同类型的事件
	events := []eventing.Event{
		makeEvent(1, "OrderAggregate", "order-1", 1, nil),
		makeEvent(2, "UserAggregate", "user-1", 1, nil),
		makeEvent(3, "ProductAggregate", "product-1", 1, nil),
	}
	for i, evt := range events {
		evt.Type = fmt.Sprintf("Event%d", i+1)
		err := store.AppendEvents(ctx, int64(i+1), toStorableEvents([]eventing.Event{evt}), 0)
		require.NoError(t, err)
	}

	t.Run("按事件类型过滤", func(t *testing.T) {
		result, err := store.GetEventStreamWithCursor(ctx, &estore.StreamOptions{
			Types: []string{"Event1", "Event2"},
			Limit: 10,
		})
		assert.NoError(t, err)
		assert.Equal(t, 2, len(result.Events))
	})

	t.Run("按聚合类型过滤", func(t *testing.T) {
		result, err := store.GetEventStreamWithCursor(ctx, &estore.StreamOptions{
			AggregateTypes: []string{"OrderAggregate", "ProductAggregate"},
			Limit:          10,
		})
		assert.NoError(t, err)
		assert.Equal(t, 2, len(result.Events))
	})
}

func TestSQLEventStore_GetEventStreamWithCursor_EmptyResult(t *testing.T) {
	db := setupTestDB(t)
	store := NewSQLEventStore(db, "domain_events")

	ctx := context.Background()

	// 空数据库查询
	result, err := store.GetEventStreamWithCursor(ctx, &estore.StreamOptions{
		Limit: 10,
	})
	assert.NoError(t, err)
	assert.Len(t, result.Events, 0)
	assert.False(t, result.HasMore)
	assert.Empty(t, result.NextCursor)
}

func TestSQLEventStore_GetEventStreamWithCursor_InvalidCursor(t *testing.T) {
	db := setupTestDB(t)
	store := NewSQLEventStore(db, "domain_events")

	ctx := context.Background()

	// 添加事件
	events := []eventing.Event{makeEvent(1, "TestAggregate", "event-1", 1, nil)}
	err := store.AppendEvents(ctx, 1, toStorableEvents(events), 0)
	require.NoError(t, err)

	// 使用不存在的游标
	result, err := store.GetEventStreamWithCursor(ctx, &estore.StreamOptions{
		After: "non-existent-cursor-id",
		Limit: 10,
	})
	assert.NoError(t, err)
	// 无效游标应该返回所有事件
	assert.GreaterOrEqual(t, len(result.Events), 1)
}

func TestSQLEventStore_DefaultTableName(t *testing.T) {
	db := setupTestDB(t)

	// 使用空表名，应该使用默认值
	store := NewSQLEventStore(db, "")
	assert.Equal(t, "domain_events", store.GetTableName())
}

func TestSQLEventStore_ConcurrencyConflict(t *testing.T) {
	db := setupTestDB(t)
	store := NewSQLEventStore(db, "domain_events")

	ctx := context.Background()
	aggregateID := int64(999)

	// 先添加一个事件
	events1 := []eventing.Event{makeEvent(aggregateID, "TestAggregate", "event-1", 1, nil)}
	err := store.AppendEvents(ctx, aggregateID, toStorableEvents(events1), 0)
	require.NoError(t, err)

	// 尝试用错误的期望版本添加
	events2 := []eventing.Event{makeEvent(aggregateID, "TestAggregate", "event-2", 2, nil)}
	err = store.AppendEvents(ctx, aggregateID, toStorableEvents(events2), 0) // 期望版本0但实际是1
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "concurrency")
}

func TestSQLEventStore_Stream_WithOptions(t *testing.T) {
	db := setupTestDB(t)
	store := NewSQLEventStore(db, "domain_events")

	ctx := context.Background()

	// 添加多个聚合类型的事件
	for i := 1; i <= 3; i++ {
		events := []eventing.Event{makeEvent(int64(i), "TypeA", fmt.Sprintf("event-a-%d", i), 1, nil)}
		err := store.AppendEvents(ctx, int64(i), toStorableEvents(events), 0)
		require.NoError(t, err)
	}
	for i := 4; i <= 6; i++ {
		events := []eventing.Event{makeEvent(int64(i), "TypeB", fmt.Sprintf("event-b-%d", i), 1, nil)}
		err := store.AppendEvents(ctx, int64(i), toStorableEvents(events), 0)
		require.NoError(t, err)
	}

	// 使用 AggregateTypes 过滤
	result, err := store.Stream(ctx, estore.StreamOptions{
		AggregateTypes: []string{"TypeA"},
		Limit:          10,
	})
	assert.NoError(t, err)
	assert.Len(t, result.Events, 3)
	for _, evt := range result.Events {
		assert.Equal(t, "TypeA", evt.AggregateType)
	}
}

func BenchmarkSQLEventStore_AppendEvents(b *testing.B) {
	db, err := basicdb.New(database.DBConfig{Driver: "sqlite", Database: ":memory:"})
	require.NoError(b, err)
	ctx := context.Background()
	_, err = db.Exec(ctx, `
        CREATE TABLE domain_events (
            id TEXT PRIMARY KEY,
            type TEXT NOT NULL,
            aggregate_id INTEGER NOT NULL,
            aggregate_type TEXT NOT NULL,
            version INTEGER NOT NULL,
            schema_version INTEGER NOT NULL,
            timestamp DATETIME NOT NULL,
            payload TEXT NOT NULL,
            metadata TEXT NOT NULL,
            UNIQUE(aggregate_id, aggregate_type, version)
        );
    `)
	require.NoError(b, err)

	store := NewSQLEventStore(db, "domain_events")
	events := []eventing.Event{makeEvent(1, "TestAggregate", "event-1", 1, nil)}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = store.AppendEvents(ctx, int64(i), toStorableEvents(events), 0)
	}
}
