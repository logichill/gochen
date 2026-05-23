package projection

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"gochen/db"
	"gochen/db/dialect"
	"gochen/db/orm"
	"gochen/db/sql/sqlbuilder"
	"gochen/errors"
)

// SQLCheckpointStore 使用现有数据库抽象持久化投影检查点。
type SQLCheckpointStore struct {
	db        db.IDatabase
	tableName string
	dialect   dialect.Dialect
}

// NewSQLCheckpointStore 创建一个基于 SQL 的检查点存储实现。
func NewSQLCheckpointStore(db db.IDatabase, tableName string) *SQLCheckpointStore {
	if tableName == "" {
		tableName = "projection_checkpoints"
	}

	return &SQLCheckpointStore{
		db:        db,
		tableName: tableName,
		dialect:   dialect.FromDatabase(db),
	}
}

// RequiresORMTxSession 声明 SQL checkpoint store 需要复用 ctx 中的 ORM 事务 session。
func (s *SQLCheckpointStore) RequiresORMTxSession() bool { return true }

// Load 读取指定投影最近一次保存的检查点。
func (s *SQLCheckpointStore) Load(ctx context.Context, projectionName string) (*Checkpoint, error) {
	sq, err := sqlbuilder.New(s.db)
	if err != nil {
		return nil, errors.Wrap(err, errors.Internal, "failed to create sql builder")
	}

	row := sq.Select(
		"projection_name", "position", "last_event_id", "last_event_time", "updated_at",
	).From(s.tableName).
		Where("projection_name = ?", projectionName).
		QueryRow(ctx)

	var checkpoint Checkpoint
	var lastEventTime sql.NullTime

	err = row.Scan(
		&checkpoint.ProjectionName,
		&checkpoint.Position,
		&checkpoint.LastEventID,
		&lastEventTime,
		&checkpoint.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, errors.NewCode(errors.NotFound, "checkpoint not found").
			WithContext("projection_name", projectionName)
	}
	if err != nil {
		return nil, errors.NewCodeWithCause(errors.Database, "checkpoint store failed", err).
			WithContext("projection_name", projectionName)
	}

	if lastEventTime.Valid {
		checkpoint.LastEventTime = lastEventTime.Time
	}

	return &checkpoint, nil
}

// Save 用 UPSERT 语义保存一条检查点记录。
func (s *SQLCheckpointStore) Save(ctx context.Context, checkpoint *Checkpoint) error {
	return s.SaveWithDB(ctx, s.databaseFor(ctx), checkpoint)
}

// SaveWithDB 使用给定数据库句柄保存检查点；可显式传入事务句柄参与同一提交边界。
func (s *SQLCheckpointStore) SaveWithDB(ctx context.Context, database db.IDatabase, checkpoint *Checkpoint) error {
	if checkpoint == nil {
		return errors.NewCode(errors.InvalidInput, "invalid checkpoint").
			WithContext("reason", "checkpoint is nil")
	}
	if !checkpoint.IsValid() {
		return errors.NewCode(errors.InvalidInput, "invalid checkpoint").
			WithContext("projection_name", checkpoint.ProjectionName).
			WithContext("position", checkpoint.Position)
	}

	// 更新 UpdatedAt
	checkpoint.UpdatedAt = time.Now()

	// 通过 ISql.UpsertInto 实现通用 UPSERT 语义
	if database == nil {
		database = s.db
	}
	sq, err := sqlbuilder.New(database)
	if err != nil {
		return errors.Wrap(err, errors.Internal, "failed to create sql builder")
	}

	_, err = sq.UpsertInto(s.tableName).
		Columns("projection_name", "position", "last_event_id", "last_event_time", "updated_at").
		Values(
			checkpoint.ProjectionName,
			checkpoint.Position,
			checkpoint.LastEventID,
			checkpoint.LastEventTime,
			checkpoint.UpdatedAt,
		).
		Key("projection_name").
		Exec(ctx)
	if err != nil {
		return errors.NewCodeWithCause(errors.Database, "checkpoint store failed", err).
			WithContext("projection_name", checkpoint.ProjectionName)
	}
	return nil
}

// Delete 删除指定投影对应的检查点记录。
func (s *SQLCheckpointStore) Delete(ctx context.Context, projectionName string) error {
	sq, err := sqlbuilder.New(s.db)
	if err != nil {
		return errors.Wrap(err, errors.Internal, "failed to create sql builder")
	}

	_, err = sq.DeleteFrom(s.tableName).
		Where("projection_name = ?", projectionName).
		Exec(ctx)
	if err != nil {
		return errors.NewCodeWithCause(errors.Database, "checkpoint store failed", err).
			WithContext("projection_name", projectionName)
	}

	return nil
}

// CreateTable 创建检查点表及必要索引。
func (s *SQLCheckpointStore) CreateTable(ctx context.Context) error {
	if err := validateCheckpointTableName(s.tableName); err != nil {
		return errors.NewCode(errors.InvalidInput, "invalid checkpoint table name").
			WithContext("table_name", s.tableName).
			WithContext("cause", err.Error())
	}

	var query string

	switch s.dialect.Name() {
	case dialect.NameSQLite:
		// 与 sqlite 迁移脚本保持一致
		query = fmt.Sprintf(`
			CREATE TABLE IF NOT EXISTS %s (
				projection_name TEXT PRIMARY KEY,
				position INTEGER NOT NULL DEFAULT 0,
				last_event_id TEXT NOT NULL DEFAULT '',
				last_event_time DATETIME NULL,
				updated_at DATETIME NOT NULL
			);
			CREATE INDEX IF NOT EXISTS idx_projection_checkpoints_updated_at ON %s(updated_at);
		`, s.tableName, s.tableName)
	case dialect.NamePostgres:
		// Postgres 兼容 DDL
		query = fmt.Sprintf(`
			CREATE TABLE IF NOT EXISTS %s (
				projection_name VARCHAR(255) PRIMARY KEY,
				position BIGINT NOT NULL DEFAULT 0,
				last_event_id VARCHAR(255) NOT NULL DEFAULT '',
				last_event_time TIMESTAMPTZ NULL,
				updated_at TIMESTAMPTZ NOT NULL
			);
			CREATE INDEX IF NOT EXISTS idx_projection_checkpoints_updated_at ON %s(updated_at);
		`, s.tableName, s.tableName)
	default:
		// 默认采用 MySQL 风格（与迁移脚本一致）
		query = fmt.Sprintf(`
			CREATE TABLE IF NOT EXISTS %s (
				projection_name VARCHAR(255) PRIMARY KEY,
				position BIGINT NOT NULL DEFAULT 0,
				last_event_id VARCHAR(255) NOT NULL DEFAULT '',
				last_event_time DATETIME NULL,
				updated_at DATETIME NOT NULL,
				INDEX idx_projection_checkpoints_updated_at (updated_at)
			) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
		`, s.tableName)
	}

	_, err := s.db.Exec(ctx, query)
	if err != nil {
		return errors.NewCodeWithCause(errors.Database, "failed to create checkpoint table", err).
			WithContext("table_name", s.tableName)
	}

	return nil
}

func (s *SQLCheckpointStore) databaseFor(ctx context.Context) db.IDatabase {
	if session, ok := orm.SessionFromContext(ctx); ok && session != nil {
		if database := session.Database(); database != nil {
			return database
		}
	}
	return s.db
}

// List 列出当前表中的全部检查点，主要用于调试和监控。
func (s *SQLCheckpointStore) List(ctx context.Context) ([]*Checkpoint, error) {
	if err := validateCheckpointTableName(s.tableName); err != nil {
		return nil, errors.NewCode(errors.InvalidInput, "invalid checkpoint table name").
			WithContext("table_name", s.tableName).
			WithContext("cause", err.Error())
	}

	sq, err := sqlbuilder.New(s.db)
	if err != nil {
		return nil, errors.Wrap(err, errors.Internal, "failed to create sql builder")
	}

	builder := sq.Select(
		"projection_name", "position", "last_event_id", "last_event_time", "updated_at",
	).From(s.tableName).
		OrderBy(sqlbuilder.OrderAsc("projection_name"))

	rows, err := builder.Query(ctx)
	if err != nil {
		return nil, errors.NewCodeWithCause(errors.Database, "checkpoint store failed", err).
			WithContext("table_name", s.tableName)
	}
	defer rows.Close()

	var checkpoints []*Checkpoint
	for rows.Next() {
		var checkpoint Checkpoint
		var lastEventTime sql.NullTime

		err := rows.Scan(
			&checkpoint.ProjectionName,
			&checkpoint.Position,
			&checkpoint.LastEventID,
			&lastEventTime,
			&checkpoint.UpdatedAt,
		)
		if err != nil {
			return nil, errors.NewCodeWithCause(errors.Database, "checkpoint store failed", err).
				WithContext("table_name", s.tableName)
		}

		if lastEventTime.Valid {
			checkpoint.LastEventTime = lastEventTime.Time
		}

		checkpoints = append(checkpoints, &checkpoint)
	}

	if err = rows.Err(); err != nil {
		return nil, errors.NewCodeWithCause(errors.Database, "checkpoint store failed", err).
			WithContext("table_name", s.tableName)
	}

	return checkpoints, nil
}

// SaveBatch 在一个事务里批量保存多条检查点记录。
func (s *SQLCheckpointStore) SaveBatch(ctx context.Context, checkpoints []*Checkpoint) error {
	if len(checkpoints) == 0 {
		return nil
	}

	if err := validateCheckpointTableName(s.tableName); err != nil {
		return errors.NewCode(errors.InvalidInput, "invalid checkpoint table name").
			WithContext("table_name", s.tableName).
			WithContext("cause", err.Error())
	}

	// 开启事务，复用 Save 的 UPSERT 语义
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return errors.NewCodeWithCause(errors.Database, "failed to begin transaction", err).
			WithContext("table_name", s.tableName)
	}
	defer tx.Rollback()

	for _, cp := range checkpoints {
		if cp == nil || !cp.IsValid() {
			continue
		}

		cp.UpdatedAt = time.Now()

		sq, err := sqlbuilder.New(tx)
		if err != nil {
			return errors.Wrap(err, errors.Internal, "failed to create sql builder")
		}

		if _, err := sq.UpsertInto(s.tableName).
			Columns("projection_name", "position", "last_event_id", "last_event_time", "updated_at").
			Values(
				cp.ProjectionName,
				cp.Position,
				cp.LastEventID,
				cp.LastEventTime,
				cp.UpdatedAt,
			).
			Key("projection_name").
			Exec(ctx); err != nil {
			return errors.NewCodeWithCause(errors.Database, "checkpoint store failed", err).
				WithContext("projection_name", cp.ProjectionName).
				WithContext("table_name", s.tableName)
		}
	}

	if err := tx.Commit(); err != nil {
		return errors.NewCodeWithCause(errors.Database, "failed to commit transaction", err).
			WithContext("table_name", s.tableName)
	}

	return nil
}

// validateCheckpointTableName 校验检查点表名，防止通过配置注入恶意 SQL 片段。
//
// 说明：
// - 约束：只允许字母、数字、下划线和点号（兼容 schema.table 写法）。
func validateCheckpointTableName(name string) error {
	if name == "" {
		return errors.NewCode(errors.InvalidInput, "table name cannot be empty")
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			r == '_' || r == '.' {
			continue
		}
		return errors.NewCode(errors.InvalidInput, "invalid character in table name").
			WithContext("char", string(r))
	}
	return nil
}

// Ensure SQLCheckpointStore implements ICheckpointStore
var _ ICheckpointStore = (*SQLCheckpointStore)(nil)
