package sqlstore_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"gochen/auth"
	obsstore "gochen/auth/sqlstore"
	"gochen/db"
	"gochen/db/sql/stdsql"
	"gochen/errors"
)

func TestAuthzLogStore_SaveAndList(t *testing.T) {
	database := setupAuthzLogSQLDB(t)
	store, err := obsstore.NewAuthzLogStore(database, "authz_logs")
	require.NoError(t, err)

	entry := auth.AuthzLogEntry{
		ID:              "log-1",
		Type:            auth.AuthzLogEntryTypeDecision,
		DecisionID:      "decision-1",
		PrincipalID:     "7",
		Permission:      "doc:read",
		Effect:          "allow",
		SnapshotVersion: "snap-v1",
		SnapshotKey:     "default",
		Consistency:     auth.ConsistencyModeStrong,
		LatencyMs:       12,
		Resources: []auth.AuthzLoggedResource{{
			Kind: "doc",
			ID:   "doc-1",
		}},
		MatchedRules: []string{"rule-1"},
		Execution:    auth.ExecutionMetadata{RequestID: "req-1"},
		Metadata:     map[string]any{"cache_hit_reason": "fresh"},
		Timestamp:    time.Now(),
	}
	require.NoError(t, store.SaveAuthzLogEntry(context.Background(), entry))

	entries, err := store.ListAuthzLogEntries(context.Background(), auth.AuthzLogEntryTypeDecision, 10)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "decision-1", entries[0].DecisionID)
	require.Equal(t, "doc:read", entries[0].Permission)
	require.Equal(t, []string{"rule-1"}, entries[0].MatchedRules)
	require.Equal(t, "req-1", entries[0].Execution.RequestID)
	require.Equal(t, "fresh", entries[0].Metadata["cache_hit_reason"])
}

func TestAuthzLogStore_RejectsUnsafeTableName(t *testing.T) {
	database := setupAuthzLogSQLDB(t)
	store, err := obsstore.NewAuthzLogStore(database, "authz_logs; DROP TABLE authz_logs")
	require.Error(t, err)
	require.Nil(t, store)
	require.True(t, errors.Is(err, errors.InvalidInput))
}

func setupAuthzLogSQLDB(t *testing.T) db.IDatabase {
	t.Helper()

	database, err := stdsql.New(db.DBConfig{Driver: "sqlite", Database: ":memory:"})
	require.NoError(t, err)

	_, err = database.Exec(context.Background(), `
		CREATE TABLE authz_logs (
			id TEXT PRIMARY KEY,
			entry_type TEXT NOT NULL,
			decision_id TEXT NOT NULL,
			principal_id TEXT NOT NULL,
			permission TEXT NOT NULL,
			operation TEXT NOT NULL,
			effect TEXT NOT NULL,
			reason_code TEXT NOT NULL,
			snapshot_version TEXT NOT NULL,
			snapshot_key TEXT NOT NULL,
			consistency TEXT NOT NULL,
			latency_ms INTEGER NOT NULL,
			cache_hit BOOLEAN NOT NULL,
			resources_json TEXT NOT NULL,
			execution_json TEXT NOT NULL,
			metadata_json TEXT NOT NULL,
			created_at DATETIME NOT NULL
		);
	`)
	require.NoError(t, err)
	return database
}
