package outbox

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"gochen/data/db"
	basicdb "gochen/data/db/basic"
	"gochen/eventing"
	"gochen/logging"
)

// MockEventStoreWithDB 模拟支持数据库接口的事件存储
type MockEventStoreWithDB struct {
	events []eventing.Event[int64]
}

func (m *MockEventStoreWithDB) Init(ctx context.Context) error {
	return nil
}

func (m *MockEventStoreWithDB) AppendEvents(ctx context.Context, aggregateID int64, events []eventing.IStorableEvent[int64], expectedVersion uint64) error {
	m.append(events)
	return nil
}

func (m *MockEventStoreWithDB) AppendEventsWithDB(ctx context.Context, db db.IDatabase, aggregateID int64, events []eventing.IStorableEvent[int64], expectedVersion uint64) error {
	m.append(events)
	return nil
}

func (m *MockEventStoreWithDB) append(events []eventing.IStorableEvent[int64]) {
	for _, evt := range events {
		if e, ok := evt.(*eventing.Event[int64]); ok {
			m.events = append(m.events, *e)
			continue
		}
		// 兜底：使用接口方法克隆事件
		payload := evt.GetPayload()
		cloned := eventing.NewEvent[int64](evt.GetAggregateID(), evt.GetAggregateType(), evt.GetType(), evt.GetVersion(), payload, evt.GetSchemaVersion())
		metadata := evt.GetMetadata()
		for k, v := range metadata {
			cloned.Metadata[k] = v
		}
		m.events = append(m.events, *cloned)
	}
}

func (m *MockEventStoreWithDB) LoadEvents(ctx context.Context, aggregateID int64, afterVersion uint64) ([]eventing.Event[int64], error) {
	return m.events, nil
}

func (m *MockEventStoreWithDB) LoadEventsByType(ctx context.Context, aggregateType string, aggregateID int64, afterVersion uint64) ([]eventing.Event[int64], error) {
	var filtered []eventing.Event[int64]
	for _, event := range m.events {
		if event.GetAggregateType() == aggregateType {
			filtered = append(filtered, event)
		}
	}
	return filtered, nil
}

func (m *MockEventStoreWithDB) StreamEvents(ctx context.Context, from time.Time) ([]eventing.Event[int64], error) {
	return m.events, nil
}

func newTestEvent(aggregateID int64, version uint64, id string, payload map[string]any) eventing.Event[int64] {
	if payload == nil {
		payload = make(map[string]any)
	}

	evt := eventing.NewEvent[int64](aggregateID, "TestAggregate", "TestEvent", version, payload)
	evt.ID = id
	evt.Metadata["source"] = "unit_test"
	return *evt
}

// 测试辅助函数：创建测试数据库
func setupTestDB(t *testing.T) db.IDatabase {
	db, err := basicdb.New(db.DBConfig{Driver: "sqlite", Database: ":memory:"})
	require.NoError(t, err)
	ctx := context.Background()
	// 创建表
	_, err = db.Exec(ctx, `
        CREATE TABLE event_outbox (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            aggregate_id INTEGER NOT NULL,
            aggregate_type TEXT NOT NULL,
            event_id TEXT NOT NULL UNIQUE,
            event_type TEXT NOT NULL,
            event_data TEXT NOT NULL,
            status TEXT NOT NULL DEFAULT 'pending',
            created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
            published_at DATETIME NULL,
            retry_count INTEGER NOT NULL DEFAULT 0,
            last_error TEXT NULL,
            next_retry_at DATETIME NULL
        );
    `)
	require.NoError(t, err)
	// 索引
	_, err = db.Exec(ctx, `CREATE INDEX idx_outbox_status_retry ON event_outbox(status, next_retry_at);`)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `CREATE INDEX idx_outbox_aggregate ON event_outbox(aggregate_id, aggregate_type);`)
	require.NoError(t, err)
	return db
}

// TestSQLOutboxRepository_SaveWithEvents 测试保存事件和 Outbox 记录
// ========== OutboxEntry 辅助方法测试 ==========

func TestOutboxEntry_TableName(t *testing.T) {
	entry := OutboxEntry{}
	assert.Equal(t, "event_outbox", entry.TableName())
}

func TestOutboxEntry_ToEvent(t *testing.T) {
	evt := newTestEvent(123, 1, "event-1", map[string]any{"value": 100})
	entry, err := EventToOutboxEntry(123, evt)
	require.NoError(t, err)

	// 反序列化回事件
	decoded, err := entry.ToEvent()
	assert.NoError(t, err)
	assert.Equal(t, evt.ID, decoded.ID)
	assert.Equal(t, evt.Type, decoded.Type)
	assert.Equal(t, evt.AggregateID, decoded.AggregateID)
}

func TestOutboxEntry_ToEvent_InvalidData(t *testing.T) {
	entry := &OutboxEntry{
		EventData: "invalid json {{{",
	}
	_, err := entry.ToEvent()
	assert.Error(t, err)
}

func TestOutboxEntry_ShouldRetry(t *testing.T) {
	maxRetries := 5

	t.Run("应该重试 - 失败且未超过最大次数", func(t *testing.T) {
		now := time.Now()
		entry := &OutboxEntry{
			Status:      OutboxStatusFailed,
			RetryCount:  2,
			NextRetryAt: &now, // 已到重试时间
		}
		time.Sleep(1 * time.Millisecond) // 确保时间已过
		assert.True(t, entry.ShouldRetry(maxRetries))
	})

	t.Run("应该重试 - NextRetryAt 为空", func(t *testing.T) {
		entry := &OutboxEntry{
			Status:      OutboxStatusFailed,
			RetryCount:  2,
			NextRetryAt: nil,
		}
		assert.True(t, entry.ShouldRetry(maxRetries))
	})

	t.Run("不应该重试 - 状态不是失败", func(t *testing.T) {
		entry := &OutboxEntry{
			Status:     OutboxStatusPending,
			RetryCount: 2,
		}
		assert.False(t, entry.ShouldRetry(maxRetries))
	})

	t.Run("不应该重试 - 超过最大重试次数", func(t *testing.T) {
		entry := &OutboxEntry{
			Status:     OutboxStatusFailed,
			RetryCount: 6, // 超过 maxRetries
		}
		assert.False(t, entry.ShouldRetry(maxRetries))
	})

	t.Run("不应该重试 - 未到重试时间", func(t *testing.T) {
		future := time.Now().Add(1 * time.Hour)
		entry := &OutboxEntry{
			Status:      OutboxStatusFailed,
			RetryCount:  2,
			NextRetryAt: &future,
		}
		assert.False(t, entry.ShouldRetry(maxRetries))
	})
}

func TestOutboxEntry_CalculateNextRetryTime(t *testing.T) {
	baseInterval := 30 * time.Second

	t.Run("第一次重试", func(t *testing.T) {
		entry := &OutboxEntry{RetryCount: 0}
		nextTime := entry.CalculateNextRetryTime(baseInterval)
		// 2^0 = 1, 应该是 baseInterval * 1 = 30s
		assert.WithinDuration(t, time.Now().Add(30*time.Second), nextTime, 1*time.Second)
	})

	t.Run("第二次重试", func(t *testing.T) {
		entry := &OutboxEntry{RetryCount: 1}
		nextTime := entry.CalculateNextRetryTime(baseInterval)
		// 2^1 = 2, 应该是 baseInterval * 2 = 60s
		assert.WithinDuration(t, time.Now().Add(60*time.Second), nextTime, 1*time.Second)
	})

	t.Run("第三次重试", func(t *testing.T) {
		entry := &OutboxEntry{RetryCount: 2}
		nextTime := entry.CalculateNextRetryTime(baseInterval)
		// 2^2 = 4, 应该是 baseInterval * 4 = 120s
		assert.WithinDuration(t, time.Now().Add(120*time.Second), nextTime, 1*time.Second)
	})

	t.Run("最大退避限制", func(t *testing.T) {
		entry := &OutboxEntry{RetryCount: 10} // 2^10 = 1024 > 32
		nextTime := entry.CalculateNextRetryTime(baseInterval)
		// 应该限制在 32 倍
		assert.WithinDuration(t, time.Now().Add(32*30*time.Second), nextTime, 1*time.Second)
	})
}

// ========== 工具函数测试 ==========

func TestEventToOutboxEntry(t *testing.T) {
	evt := newTestEvent(123, 1, "event-1", map[string]any{
		"user":  "alice",
		"value": 100,
	})

	entry, err := EventToOutboxEntry(123, evt)
	assert.NoError(t, err)
	assert.NotNil(t, entry)
	assert.Equal(t, int64(123), entry.AggregateID)
	assert.Equal(t, "TestEvent", entry.EventType)
	assert.Equal(t, OutboxStatusPending, entry.Status)
	assert.NotEmpty(t, entry.EventData)
}

func TestDefaultOutboxConfig(t *testing.T) {
	cfg := DefaultOutboxConfig()
	assert.Equal(t, 5*time.Second, cfg.PublishInterval)
	assert.Equal(t, 100, cfg.BatchSize)
	assert.Equal(t, 5, cfg.MaxRetries)
	assert.Equal(t, 30*time.Second, cfg.RetryInterval)
	assert.Equal(t, 1*time.Hour, cfg.CleanupInterval)
	assert.Equal(t, 7*24*time.Hour, cfg.RetentionPeriod)
}

// ========== 仓储测试 ==========

func TestSQLOutboxRepository_SaveWithEvents(t *testing.T) {
	db := setupTestDB(t)
	eventStore := &MockEventStoreWithDB{}
	repo := NewSimpleSQLOutboxRepository(db, eventStore, logging.NewNoopLogger())

	ctx := context.Background()
	aggregateID := int64(123)

	// 创建测试事件
	events := []eventing.Event[int64]{
		newTestEvent(aggregateID, 1, "event-1", map[string]any{"value": 100}),
		newTestEvent(aggregateID, 2, "event-2", map[string]any{"value": 200}),
	}

	// 保存事件和 Outbox 记录
	err := repo.SaveWithEvents(ctx, aggregateID, events)
	assert.NoError(t, err)

	// 验证事件已保存到事件存储
	assert.Len(t, eventStore.events, 2)
	assert.Equal(t, "event-1", eventStore.events[0].GetID())
	assert.Equal(t, "event-2", eventStore.events[1].GetID())

	// 验证 Outbox 记录已创建
	entries, err := repo.GetPendingEntries(ctx, 10)
	assert.NoError(t, err)
	assert.Len(t, entries, 2)

	// 验证第一个 Outbox 记录
	entry1 := entries[0]
	assert.Equal(t, aggregateID, entry1.AggregateID)
	assert.Equal(t, "TestAggregate", entry1.AggregateType)
	assert.Equal(t, "event-1", entry1.EventID)
	assert.Equal(t, "TestEvent", entry1.EventType)
	assert.Equal(t, OutboxStatusPending, entry1.Status)
	assert.Equal(t, 0, entry1.RetryCount)
	assert.Nil(t, entry1.PublishedAt)
	assert.Nil(t, entry1.NextRetryAt)

	// 验证事件数据可以反序列化
	assert.NotEmpty(t, entry1.EventData)
}

// TestSQLOutboxRepository_GetPendingEntries 测试获取待发布记录
func TestSQLOutboxRepository_GetPendingEntries(t *testing.T) {
	db := setupTestDB(t)
	eventStore := &MockEventStoreWithDB{}
	repo := NewSimpleSQLOutboxRepository(db, eventStore, logging.NewNoopLogger())

	ctx := context.Background()

	// 先保存一些事件
	events := []eventing.Event[int64]{
		newTestEvent(1, 1, "event-1", nil),
		newTestEvent(2, 1, "event-2", nil),
	}

	err := repo.SaveWithEvents(ctx, 1, events[:1])
	assert.NoError(t, err)

	err = repo.SaveWithEvents(ctx, 2, events[1:])
	assert.NoError(t, err)

	// 获取待发布记录
	entries, err := repo.GetPendingEntries(ctx, 10)
	assert.NoError(t, err)
	assert.Len(t, entries, 2)

	// 验证按创建时间排序
	assert.Equal(t, "event-1", entries[0].EventID)
	assert.Equal(t, "event-2", entries[1].EventID)

	// 测试限制数量
	entries, err = repo.GetPendingEntries(ctx, 1)
	assert.NoError(t, err)
	assert.Len(t, entries, 1)
	assert.Equal(t, "event-1", entries[0].EventID)
}

// TestSQLOutboxRepository_MarkAsPublished 测试标记为已发布
func TestSQLOutboxRepository_MarkAsPublished(t *testing.T) {
	db := setupTestDB(t)
	eventStore := &MockEventStoreWithDB{}
	repo := NewSimpleSQLOutboxRepository(db, eventStore, logging.NewNoopLogger())

	ctx := context.Background()

	// 先保存一个事件
	events := []eventing.Event[int64]{
		newTestEvent(1, 1, "event-1", nil),
	}

	err := repo.SaveWithEvents(ctx, 1, events)
	assert.NoError(t, err)

	// 获取 Outbox 记录
	entries, err := repo.GetPendingEntries(ctx, 1)
	assert.NoError(t, err)
	assert.Len(t, entries, 1)

	entryID := entries[0].ID

	// 标记为已发布
	err = repo.MarkAsPublished(ctx, entryID)
	assert.NoError(t, err)

	// 验证状态已更新
	entries, err = repo.GetPendingEntries(ctx, 10)
	assert.NoError(t, err)
	assert.Len(t, entries, 0) // 已发布的记录不应该出现在待发布列表中
}

// TestSQLOutboxRepository_MarkAsFailed 测试标记为失败
func TestSQLOutboxRepository_MarkAsFailed(t *testing.T) {
	db := setupTestDB(t)
	eventStore := &MockEventStoreWithDB{}
	repo := NewSimpleSQLOutboxRepository(db, eventStore, logging.NewNoopLogger())

	ctx := context.Background()

	// 先保存一个事件
	events := []eventing.Event[int64]{
		newTestEvent(1, 1, "event-1", nil),
	}

	err := repo.SaveWithEvents(ctx, 1, events)
	assert.NoError(t, err)

	// 获取 Outbox 记录
	entries, err := repo.GetPendingEntries(ctx, 1)
	assert.NoError(t, err)
	assert.Len(t, entries, 1)

	entryID := entries[0].ID
	errorMsg := "test error"
	nextRetryAt := time.Now().Add(time.Minute)

	// 标记为失败
	err = repo.MarkAsFailed(ctx, entryID, errorMsg, nextRetryAt)
	assert.NoError(t, err)

	// 失败记录在达到 next_retry_at 前不应返回
	entries, err = repo.GetPendingEntries(ctx, 10)
	assert.NoError(t, err)
	assert.Len(t, entries, 0)

	// 设置下一次重试时间为过去，使其可再次被拉取
	pastRetry := time.Now().Add(-time.Minute)
	err = repo.MarkAsFailed(ctx, entryID, errorMsg, pastRetry)
	assert.NoError(t, err)

	entries, err = repo.GetPendingEntries(ctx, 10)
	assert.NoError(t, err)
	assert.Len(t, entries, 1)

	entry := entries[0]
	assert.Equal(t, OutboxStatusFailed, entry.Status)
	assert.Equal(t, errorMsg, entry.LastError)
	assert.Equal(t, 2, entry.RetryCount)
	assert.NotNil(t, entry.NextRetryAt)
}

// TestSQLOutboxRepository_DeletePublished 测试删除已发布记录
func TestSQLOutboxRepository_DeletePublished(t *testing.T) {
	db := setupTestDB(t)
	eventStore := &MockEventStoreWithDB{}
	repo := NewSimpleSQLOutboxRepository(db, eventStore, logging.NewNoopLogger())

	ctx := context.Background()

	// 先保存一个事件
	events := []eventing.Event[int64]{
		newTestEvent(1, 1, "event-1", nil),
	}

	err := repo.SaveWithEvents(ctx, 1, events)
	assert.NoError(t, err)

	// 获取并标记为已发布
	entries, err := repo.GetPendingEntries(ctx, 1)
	assert.NoError(t, err)
	assert.Len(t, entries, 1)

	err = repo.MarkAsPublished(ctx, entries[0].ID)
	assert.NoError(t, err)

	// 删除已发布记录
	olderThan := time.Now().Add(time.Hour) // 删除一小时前的记录
	err = repo.DeletePublished(ctx, olderThan)
	assert.NoError(t, err)

	// 验证记录已删除（通过直接查询数据库）
	var count int64
	row := db.QueryRow(ctx, `SELECT COUNT(1) FROM event_outbox`)
	if err := row.Scan(&count); err != nil {
		t.Fatalf("count scan error: %v", err)
	}
	assert.Equal(t, int64(0), count)
}

// TestSQLOutboxRepository_EnsureTable 测试表创建
func TestSQLOutboxRepository_EnsureTable(t *testing.T) {
	t.Skip("EnsureTable 使用 MySQL 语法，当前测试环境为 SQLite/basic 实现，跳过")
}
