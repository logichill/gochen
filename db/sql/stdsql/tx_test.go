package stdsql

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gochen/db"

	_ "modernc.org/sqlite"
)

func TestTx_Close_RollbacksWhenNotCommitted(t *testing.T) {
	ctx := context.Background()
	db, err := NewWithContext(ctx, db.DBConfig{
		Driver:       "sqlite",
		Database:     ":memory:",
		MaxOpenConns: 1,
	})
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	_, err = db.Exec(ctx, "CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)")
	require.NoError(t, err)

	tx, err := db.Begin(ctx)
	require.NoError(t, err)

	_, err = tx.Exec(ctx, "INSERT INTO t(v) VALUES (?)", "x")
	require.NoError(t, err)

	require.NoError(t, tx.Close())

	var count int
	require.NoError(t, db.QueryRow(ctx, "SELECT COUNT(*) FROM t").Scan(&count))
	assert.Equal(t, 0, count)
}

func TestTx_Close_IsIdempotent(t *testing.T) {
	ctx := context.Background()
	db, err := NewWithContext(ctx, db.DBConfig{
		Driver:       "sqlite",
		Database:     ":memory:",
		MaxOpenConns: 1,
	})
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	_, err = db.Exec(ctx, "CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)")
	require.NoError(t, err)

	tx, err := db.Begin(ctx)
	require.NoError(t, err)

	require.NoError(t, tx.Close())
	require.NoError(t, tx.Close())
}

func TestTx_Close_AfterCommit_IsNoop(t *testing.T) {
	ctx := context.Background()
	db, err := NewWithContext(ctx, db.DBConfig{
		Driver:       "sqlite",
		Database:     ":memory:",
		MaxOpenConns: 1,
	})
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	_, err = db.Exec(ctx, "CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)")
	require.NoError(t, err)

	tx, err := db.Begin(ctx)
	require.NoError(t, err)

	_, err = tx.Exec(ctx, "INSERT INTO t(v) VALUES (?)", "x")
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	require.NoError(t, tx.Close())

	var count int
	require.NoError(t, db.QueryRow(ctx, "SELECT COUNT(*) FROM t").Scan(&count))
	assert.Equal(t, 1, count)
}
