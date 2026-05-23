package projection

import (
	"context"
	stderrors "errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"gochen/db"
	"gochen/db/orm"
	ormlite "gochen/db/orm/lite"
	basicdb "gochen/db/sql/stdsql"
	"gochen/errors"
	"gochen/eventing"
)

func TestCheckpointingProjector_SQLCheckpointStoreRequiresOrmTxSession(t *testing.T) {
	projector := NewMockProjection("sql-checkpoint-no-tx", []string{"TestEvent"})
	wrapped, err := NewCheckpointingProjector[int64](projector, &checkpointingTxRunner{})
	require.NoError(t, err)

	database := newProjectionCheckpointTestDB(t)
	store := NewSQLCheckpointStore(database, "projection_checkpoints")
	require.NoError(t, store.CreateTable(context.Background()))

	evt := eventing.NewEvent[int64](1, "Agg", "TestEvent", 1, nil)
	err = wrapped.HandleWithCheckpoint(context.Background(), evt, store, NewCheckpoint(wrapped.Name(), 1, evt.ID, evt.Timestamp))
	require.Error(t, err)
	require.Equal(t, errors.Unsupported, errors.Code(err))
}

type decoratedCheckpointStore struct {
	ICheckpointStore
	requiresTx bool
}

func (s decoratedCheckpointStore) RequiresORMTxSession() bool {
	return s.requiresTx
}

func TestCheckpointingProjector_DecoratedSQLCheckpointStoreRequiresOrmTxSession(t *testing.T) {
	projector := NewMockProjection("sql-checkpoint-decorated-no-tx", []string{"TestEvent"})
	wrapped, err := NewCheckpointingProjector[int64](projector, &checkpointingTxRunner{})
	require.NoError(t, err)

	database := newProjectionCheckpointTestDB(t)
	baseStore := NewSQLCheckpointStore(database, "projection_checkpoints")
	require.NoError(t, baseStore.CreateTable(context.Background()))

	store := decoratedCheckpointStore{
		ICheckpointStore: baseStore,
		requiresTx:       true,
	}
	evt := eventing.NewEvent[int64](1, "Agg", "TestEvent", 1, nil)
	err = wrapped.HandleWithCheckpoint(context.Background(), evt, store, NewCheckpoint(wrapped.Name(), 1, evt.ID, evt.Timestamp))
	require.Error(t, err)
	require.Equal(t, errors.Unsupported, errors.Code(err))
}

func TestCheckpointingProjector_SQLCheckpointFailureRollsBackReadModel(t *testing.T) {
	ctx := context.Background()
	database := newProjectionCheckpointTestDB(t)
	ormEngine := newProjectionCheckpointTestOrm(t, database)
	txRunner, err := NewSQLCheckpointTxRunner(ormEngine)
	require.NoError(t, err)

	projector := NewMockProjection("sql-checkpoint-rollback", []string{"TestEvent"})
	projector.handleFunc = func(ctx context.Context, event eventing.IEvent) error {
		session, ok := orm.SessionFromContext(ctx)
		require.True(t, ok)
		_, err := session.Database().Exec(ctx, `INSERT INTO projection_read_model (id, value) VALUES (?, ?)`, event.GetID(), "written")
		return err
	}
	wrapped, err := NewCheckpointingProjector[int64](projector, txRunner)
	require.NoError(t, err)

	store := NewSQLCheckpointStore(database, "missing_projection_checkpoints")
	evt := eventing.NewEvent[int64](1, "Agg", "TestEvent", 1, nil)
	err = wrapped.HandleWithCheckpoint(ctx, evt, store, NewCheckpoint(wrapped.Name(), 1, evt.ID, evt.Timestamp))
	require.Error(t, err)
	require.Zero(t, projectionReadModelCount(t, database))
}

func TestCheckpointingProjector_ReadModelFailureDoesNotSaveSQLCheckpoint(t *testing.T) {
	ctx := context.Background()
	database := newProjectionCheckpointTestDB(t)
	ormEngine := newProjectionCheckpointTestOrm(t, database)
	txRunner, err := NewSQLCheckpointTxRunner(ormEngine)
	require.NoError(t, err)

	store := NewSQLCheckpointStore(database, "projection_checkpoints")
	require.NoError(t, store.CreateTable(ctx))

	wantErr := stderrors.New("read model failed")
	projector := NewMockProjection("sql-checkpoint-read-fail", []string{"TestEvent"})
	projector.handleFunc = func(ctx context.Context, event eventing.IEvent) error {
		return wantErr
	}
	wrapped, err := NewCheckpointingProjector[int64](projector, txRunner)
	require.NoError(t, err)

	evt := eventing.NewEvent[int64](1, "Agg", "TestEvent", 1, nil)
	err = wrapped.HandleWithCheckpoint(ctx, evt, store, NewCheckpoint(wrapped.Name(), 1, evt.ID, time.Now()))
	require.ErrorIs(t, err, wantErr)

	_, err = store.Load(ctx, wrapped.Name())
	require.Equal(t, errors.NotFound, errors.Code(err))
}

func newProjectionCheckpointTestDB(tb testing.TB) db.IDatabase {
	tb.Helper()

	database, err := basicdb.New(db.DBConfig{
		Driver:       "sqlite",
		Database:     ":memory:",
		MaxOpenConns: 1,
		MaxIdleConns: 1,
	})
	require.NoError(tb, err)
	tb.Cleanup(func() { _ = database.Close() })

	_, err = database.Exec(context.Background(), `
		CREATE TABLE projection_read_model (
			id TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)
	`)
	require.NoError(tb, err)
	return database
}

func newProjectionCheckpointTestOrm(tb testing.TB, database db.IDatabase) orm.IOrm {
	tb.Helper()

	ormEngine, err := ormlite.New(database)
	require.NoError(tb, err)
	return ormEngine
}

func projectionReadModelCount(tb testing.TB, database db.IDatabase) int {
	tb.Helper()

	var count int
	require.NoError(tb, database.QueryRow(context.Background(), `SELECT COUNT(1) FROM projection_read_model`).Scan(&count))
	return count
}
