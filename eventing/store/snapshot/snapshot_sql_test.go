package snapshot

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"gochen/db"
	"gochen/db/sql/stdsql"
	"gochen/errors"
)

func TestSQLStore_RejectsUnsafeTableName(t *testing.T) {
	database, err := stdsql.New(db.DBConfig{Driver: "sqlite", Database: ":memory:"})
	require.NoError(t, err)
	defer database.Close()

	store := NewSQLStore(database, "event_snapshots; DROP TABLE event_snapshots")
	err = store.SaveSnapshot(context.Background(), Snapshot[int64]{
		AggregateID:   1,
		AggregateType: "counter",
		Version:       1,
		Data:          []byte(`{}`),
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
}
