package outbox

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"gochen/db"
	basicdb "gochen/db/sql/stdsql"
	"gochen/eventing"
	"gochen/eventing/registry"
	"gochen/eventing/upcast"
)

type testEventPayload struct {
	Value int `json:"value"`
}

func newTestRegistry(tb testing.TB) *registry.Registry {
	tb.Helper()
	reg := registry.NewRegistry()
	require.NoError(tb, reg.Register("TestEvent", func() any { return &testEventPayload{} }))
	return reg
}

func newTestUpgraders() *upcast.UpgraderRegistry {
	return upcast.NewUpgraderRegistry()
}

// newTestEvent aggregateID：对象/实体标识。
//
// 参数：
// - version：版本号（类型：uint64）
// - id：对象/实体标识
// - payload：属性/参数集合（类型：map[string]any）
//
// 返回：
// - result：测试返回值（类型：eventing.Event[int64]）
func newTestEvent(aggregateID int64, version uint64, id string, payload map[string]any) eventing.Event[int64] {
	if payload == nil {
		payload = make(map[string]any)
	}

	evt := eventing.NewEvent[int64](aggregateID, "TestAggregate", "TestEvent", version, payload)
	evt.ID = id
	evt.GetMetadata().Set("source", "unit_test")
	return *evt
}

func setupTestDB(tb testing.TB) db.IDatabase {
	tb.Helper()

	database, err := basicdb.New(db.DBConfig{Driver: "sqlite", Database: ":memory:"})
	require.NoError(tb, err)

	ctx := context.Background()
	// 直接创建测试表，不依赖 EnsureTable
	_, err = database.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS event_outbox (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			aggregate_id INTEGER NOT NULL,
			aggregate_type TEXT NOT NULL,
			event_id TEXT NOT NULL UNIQUE,
			event_type TEXT NOT NULL,
			event_data TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			claim_token TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			published_at DATETIME NULL,
			retry_count INTEGER NOT NULL DEFAULT 0,
			last_error TEXT NULL,
			lease_until DATETIME NULL,
			next_retry_at DATETIME NULL
		)
	`)
	require.NoError(tb, err)

	return database
}
