package repo

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"gochen/db"
	"gochen/db/orm"
	ormbasic "gochen/db/orm/lite"
	basicdb "gochen/db/sql/stdsql"
	"gochen/domain/audited"
	"gochen/errors"
)

func setupAuditStore(tb testing.TB, tableName string) (*AuditStore, db.IDatabase) {
	tb.Helper()

	database, err := basicdb.New(db.DBConfig{
		Driver:       "sqlite",
		Database:     ":memory:",
		MaxOpenConns: 1,
		MaxIdleConns: 1,
	})
	require.NoError(tb, err)
	tb.Cleanup(func() { _ = database.Close() })

	ctx := context.Background()
	_, err = database.Exec(ctx, `
CREATE TABLE `+tableName+` (
  id INTEGER PRIMARY KEY,
  entity_id TEXT NOT NULL,
  operation TEXT NOT NULL,
  operator TEXT NOT NULL,
  timestamp DATETIME NOT NULL,
  changes TEXT NOT NULL,
  metadata TEXT NOT NULL
);`)
	require.NoError(tb, err)

	ormEngine, err := ormbasic.New(database)
	require.NoError(tb, err)
	store, err := NewAuditStore(ormEngine, tableName)
	require.NoError(tb, err)
	return store, database
}

// TestAuditStore_SaveAuditRecord_ValidatesInput 验证 AuditStore SaveAuditRecord ValidatesInput。
func TestAuditStore_SaveAuditRecord_ValidatesInput(t *testing.T) {
	store, _ := setupAuditStore(t, "audit_records")

	t.Run("empty entity id", func(t *testing.T) {
		_, err := store.SaveAuditRecord(context.Background(), audited.AuditRecord{
			EntityID:  "",
			Operation: audited.AuditOpCreate,
			Operator:  "alice",
		})
		require.Error(t, err)
		require.True(t, errors.Is(err, errors.InvalidInput))
	})

	t.Run("empty operation", func(t *testing.T) {
		_, err := store.SaveAuditRecord(context.Background(), audited.AuditRecord{
			EntityID:  "1",
			Operation: "",
			Operator:  "alice",
		})
		require.Error(t, err)
		require.True(t, errors.Is(err, errors.InvalidInput))
	})

	t.Run("empty operator", func(t *testing.T) {
		_, err := store.SaveAuditRecord(context.Background(), audited.AuditRecord{
			EntityID:  "1",
			Operation: audited.AuditOpCreate,
			Operator:  "",
		})
		require.Error(t, err)
		require.True(t, errors.Is(err, errors.InvalidInput))
	})
}

// TestAuditStore_SaveAuditRecord_AndList 验证 AuditStore SaveAuditRecord AndList。
func TestAuditStore_SaveAuditRecord_AndList(t *testing.T) {
	store, _ := setupAuditStore(t, "audit_records")

	ctx := context.Background()
	id, err := store.SaveAuditRecord(ctx, audited.AuditRecord{
		EntityID:  "1",
		Operation: audited.AuditOpCreate,
		Operator:  "alice",
		Changes:   json.RawMessage(`{"title":"hello"}`),
		Metadata:  json.RawMessage(`{"source":"unit_test"}`),
	})
	require.NoError(t, err)
	require.NotZero(t, id)

	records, err := store.ListAuditRecordsByEntity(ctx, "1", 0, 10)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, id, records[0].ID)
	require.Equal(t, "1", records[0].EntityID)
	require.Equal(t, audited.AuditOpCreate, records[0].Operation)
	require.Equal(t, "alice", records[0].Operator)
	require.False(t, records[0].Timestamp.IsZero())
	var changes map[string]string
	require.NoError(t, json.Unmarshal(records[0].Changes, &changes))
	require.Equal(t, "hello", changes["title"])
	var metadata map[string]string
	require.NoError(t, json.Unmarshal(records[0].Metadata, &metadata))
	require.Equal(t, "unit_test", metadata["source"])
}

// TestAuditStore_ListAuditRecordsByEntity_PaginationAndOrdering 验证 AuditStore ListAuditRecordsByEntity PaginationAndOrdering。
func TestAuditStore_ListAuditRecordsByEntity_PaginationAndOrdering(t *testing.T) {
	store, _ := setupAuditStore(t, "audit_records")

	ctx := context.Background()
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)

	// timestamp desc, id desc
	_, err := store.SaveAuditRecord(ctx, audited.AuditRecord{ID: 10, EntityID: "1", Operation: "A", Operator: "alice", Timestamp: t1})
	require.NoError(t, err)
	_, err = store.SaveAuditRecord(ctx, audited.AuditRecord{ID: 20, EntityID: "1", Operation: "B", Operator: "alice", Timestamp: t2})
	require.NoError(t, err)
	_, err = store.SaveAuditRecord(ctx, audited.AuditRecord{ID: 30, EntityID: "1", Operation: "C", Operator: "alice", Timestamp: t2})
	require.NoError(t, err)

	all, err := store.ListAuditRecordsByEntity(ctx, "1", 0, 10)
	require.NoError(t, err)
	require.Len(t, all, 3)
	require.Equal(t, int64(30), all[0].ID)
	require.Equal(t, int64(20), all[1].ID)
	require.Equal(t, int64(10), all[2].ID)

	page, err := store.ListAuditRecordsByEntity(ctx, "1", 1, 1)
	require.NoError(t, err)
	require.Len(t, page, 1)
	require.Equal(t, int64(20), page[0].ID)

	// negative offset coerces to 0; non-positive limit defaults to 100.
	page2, err := store.ListAuditRecordsByEntity(ctx, "1", -10, 0)
	require.NoError(t, err)
	require.Len(t, page2, 3)
}

// TestAuditStore_ListAuditRecordsByEntity_InvalidJSONReturnsInternal 验证 AuditStore ListAuditRecordsByEntity InvalidJSONReturnsInternal。
func TestAuditStore_ListAuditRecordsByEntity_InvalidJSONReturnsInternal(t *testing.T) {
	store, database := setupAuditStore(t, "audit_records")

	ctx := context.Background()
	_, err := database.Exec(ctx,
		"INSERT INTO audit_records (id, entity_id, operation, operator, timestamp, changes, metadata) VALUES (?, ?, ?, ?, ?, ?, ?)",
		int64(1), "1", "CREATE", "alice", time.Now(), "{bad_json", "{}",
	)
	require.NoError(t, err)

	_, err = store.ListAuditRecordsByEntity(ctx, "1", 0, 10)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Internal))
}

func TestAuditStore_SaveAuditRecord_UsesTxSessionFromContext(t *testing.T) {
	store, database := setupAuditStore(t, "audit_records")

	ormEngine, err := ormbasic.New(database)
	require.NoError(t, err)
	session, err := ormEngine.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	defer func() { _ = session.Rollback() }()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	txCtx, err := orm.WithTxSession(ctx, session, true)
	require.NoError(t, err)
	_, err = store.SaveAuditRecord(txCtx, audited.AuditRecord{
		EntityID:  "1",
		Operation: audited.AuditOpCreate,
		Operator:  "alice",
	})
	require.NoError(t, err)

	require.NoError(t, session.Rollback())

	records, err := store.ListAuditRecordsByEntity(context.Background(), "1", 0, 10)
	require.NoError(t, err)
	require.Len(t, records, 0, "rollback should discard audit record written in tx")
}
