package sqlstore_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"gochen/auth"
	storesql "gochen/auth/sqlstore"
	"gochen/db"
	basicdb "gochen/db/sql/stdsql"
	"gochen/errors"
)

func TestPolicySnapshotStore_SaveLoadListCleanup(t *testing.T) {
	database := setupAuthzSQLDB(t)
	store, err := storesql.NewPolicySnapshotStore(database, "authz_policy_snapshots")
	require.NoError(t, err)

	require.NoError(t, store.SavePolicySnapshot(context.Background(), auth.PolicySnapshot{
		Key:       "default",
		Version:   "v1",
		Timestamp: time.Now().Add(-2 * time.Hour),
		Metadata:  map[string]any{"source": "seed"},
	}))
	require.NoError(t, store.SavePolicySnapshot(context.Background(), auth.PolicySnapshot{
		Key:       "tenant:1",
		Version:   "v2",
		Timestamp: time.Now(),
	}))

	loaded, err := store.LoadPolicySnapshot(context.Background(), "default")
	require.NoError(t, err)
	require.Equal(t, "v1", loaded.Version)
	require.Equal(t, "seed", loaded.Metadata["source"])

	list, err := store.ListPolicySnapshots(context.Background(), "tenant:", 10)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, "tenant:1", list[0].Key)

	require.NoError(t, store.CleanupPolicySnapshots(context.Background(), time.Hour))
	_, err = store.LoadPolicySnapshot(context.Background(), "default")
	require.True(t, errors.Is(err, errors.NotFound))
}

func TestPolicySnapshotStore_RejectsUnsafeTableName(t *testing.T) {
	database := setupAuthzSQLDB(t)
	store, err := storesql.NewPolicySnapshotStore(database, "authz_policy_snapshots; DROP TABLE authz_policy_snapshots")
	require.Error(t, err)
	require.Nil(t, store)
	require.True(t, errors.Is(err, errors.InvalidInput))
}

func setupAuthzSQLDB(t *testing.T) db.IDatabase {
	t.Helper()

	database, err := basicdb.New(db.DBConfig{Driver: "sqlite", Database: ":memory:"})
	require.NoError(t, err)

	_, err = database.Exec(context.Background(), `
		CREATE TABLE authz_policy_snapshots (
			snapshot_key TEXT PRIMARY KEY,
			version TEXT NOT NULL,
			timestamp DATETIME NOT NULL,
			metadata TEXT NOT NULL
		);
	`)
	require.NoError(t, err)
	return database
}
