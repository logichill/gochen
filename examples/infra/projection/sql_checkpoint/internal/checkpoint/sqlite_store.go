package checkpoint

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"gochen/eventing/projection"
	dbcore "gochen/storage/database"
)

// SQLiteCheckpointStore 示例级 SQLite 检查点存储（演示用）
type SQLiteCheckpointStore struct {
	db        dbcore.IDatabase
	tableName string
}

func NewSQLiteCheckpointStore(db dbcore.IDatabase, table string) *SQLiteCheckpointStore {
	if table == "" {
		table = "projection_checkpoints"
	}
	return &SQLiteCheckpointStore{db: db, tableName: table}
}

func (s *SQLiteCheckpointStore) EnsureTable(ctx context.Context) error {
	ddl := fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s (
	projection_name TEXT PRIMARY KEY,
	position INTEGER NOT NULL DEFAULT 0,
	last_event_id TEXT NOT NULL DEFAULT '',
	last_event_time DATETIME NULL,
	updated_at DATETIME NOT NULL
)`, s.tableName)
	_, err := s.db.Exec(ctx, ddl)
	return err
}

func (s *SQLiteCheckpointStore) Load(ctx context.Context, name string) (*projection.Checkpoint, error) {
	q := fmt.Sprintf(`SELECT projection_name, position, last_event_id, last_event_time, updated_at FROM %s WHERE projection_name = ?`, s.tableName)
	row := s.db.QueryRow(ctx, q, name)
	var cp projection.Checkpoint
	var last sql.NullTime
	if err := row.Scan(&cp.ProjectionName, &cp.Position, &cp.LastEventID, &last, &cp.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, projection.ErrCheckpointNotFound
		}
		return nil, fmt.Errorf("load checkpoint: %w", err)
	}
	if last.Valid {
		cp.LastEventTime = last.Time
	}
	return &cp, nil
}

func (s *SQLiteCheckpointStore) Save(ctx context.Context, cp *projection.Checkpoint) error {
	if cp == nil || !cp.IsValid() {
		return projection.ErrInvalidCheckpoint
	}
	cp.UpdatedAt = time.Now()
	// SQLite UPSERT
	q := fmt.Sprintf(`
INSERT INTO %s (projection_name, position, last_event_id, last_event_time, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(projection_name) DO UPDATE SET
	position=excluded.position,
	last_event_id=excluded.last_event_id,
	last_event_time=excluded.last_event_time,
	updated_at=excluded.updated_at`, s.tableName)
	_, err := s.db.Exec(ctx, q, cp.ProjectionName, cp.Position, cp.LastEventID, cp.LastEventTime, cp.UpdatedAt)
	if err != nil {
		return fmt.Errorf("save checkpoint: %w", err)
	}
	return nil
}

func (s *SQLiteCheckpointStore) Delete(ctx context.Context, name string) error {
	q := fmt.Sprintf(`DELETE FROM %s WHERE projection_name = ?`, s.tableName)
	_, err := s.db.Exec(ctx, q, name)
	if err != nil {
		return fmt.Errorf("delete checkpoint: %w", err)
	}
	return nil
}

// Ensure interface compliance
var _ projection.ICheckpointStore = (*SQLiteCheckpointStore)(nil)
