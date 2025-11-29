package projection

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"gochen/data/db"
	"gochen/data/db/dialect"
	sqlbuilder "gochen/data/db/sql"
)

// SQLCheckpointStore SQL 检查点存储实现
//
// 使用现有的 storage.IDatabase 接口实现检查点持久化。
//
// 特性：
//   - 复用现有数据库抽象
//   - 支持事务
//   - UPSERT 语义（幂等）
//   - 线程安全
type SQLCheckpointStore struct {
	db        database.IDatabase
	tableName string
	dialect   dialect.Dialect
}

// NewSQLCheckpointStore 创建 SQL 检查点存储
//
// 参数：
//   - db: 数据库实例（复用现有抽象）
//   - tableName: 表名（默认 "projection_checkpoints"）
//
// 返回：
//   - *SQLCheckpointStore: 存储实例
func NewSQLCheckpointStore(db database.IDatabase, tableName string) *SQLCheckpointStore {
	if tableName == "" {
		tableName = "projection_checkpoints"
	}

	return &SQLCheckpointStore{
		db:        db,
		tableName: tableName,
		dialect:   dialect.FromDatabase(db),
	}
}

// Load 加载检查点
func (s *SQLCheckpointStore) Load(ctx context.Context, projectionName string) (*Checkpoint, error) {
	row := sqlbuilder.New(s.db).Select(
		"projection_name", "position", "last_event_id", "last_event_time", "updated_at",
	).From(s.tableName).
		Where("projection_name = ?", projectionName).
		QueryRow(ctx)

	var checkpoint Checkpoint
	var lastEventTime sql.NullTime

	err := row.Scan(
		&checkpoint.ProjectionName,
		&checkpoint.Position,
		&checkpoint.LastEventID,
		&lastEventTime,
		&checkpoint.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, ErrCheckpointNotFound
	}
	if err != nil {
		return nil, errors.Join(ErrCheckpointStoreFailed, err)
	}

	if lastEventTime.Valid {
		checkpoint.LastEventTime = lastEventTime.Time
	}

	return &checkpoint, nil
}

// Save 保存检查点（使用 UPSERT 语义）
func (s *SQLCheckpointStore) Save(ctx context.Context, checkpoint *Checkpoint) error {
	if checkpoint == nil || !checkpoint.IsValid() {
		return ErrInvalidCheckpoint
	}

	// 更新 UpdatedAt
	checkpoint.UpdatedAt = time.Now()

	// 通过 ISql.UpsertInto 实现通用 UPSERT 语义
	_, err := sqlbuilder.New(s.db).UpsertInto(s.tableName).
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
		return errors.Join(ErrCheckpointStoreFailed, err)
	}
	return nil
}

// Delete 删除检查点
func (s *SQLCheckpointStore) Delete(ctx context.Context, projectionName string) error {
	_, err := sqlbuilder.New(s.db).DeleteFrom(s.tableName).
		Where("projection_name = ?", projectionName).
		Exec(ctx)
	if err != nil {
		return errors.Join(ErrCheckpointStoreFailed, err)
	}

	return nil
}

// CreateTable 创建检查点表
//
// 用于初始化数据库表结构。
//
// 参数：
//   - ctx: 上下文
//
// 返回：
//   - error: 创建失败错误
//
// 注意：
//   - 使用 IF NOT EXISTS 确保幂等性
//   - 表结构与 Checkpoint 结构体对应
func (s *SQLCheckpointStore) CreateTable(ctx context.Context) error {
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
		return fmt.Errorf("failed to create checkpoint table: %w", err)
	}

	return nil
}

// List 列出所有检查点
//
// 用于监控和调试。
//
// 参数：
//   - ctx: 上下文
//
// 返回：
//   - []*Checkpoint: 检查点列表
//   - error: 查询失败错误
func (s *SQLCheckpointStore) List(ctx context.Context) ([]*Checkpoint, error) {
	builder := sqlbuilder.New(s.db).Select(
		"projection_name", "position", "last_event_id", "last_event_time", "updated_at",
	).From(s.tableName).
		OrderBy("projection_name")

	rows, err := builder.Query(ctx)
	if err != nil {
		return nil, errors.Join(ErrCheckpointStoreFailed, err)
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
			return nil, errors.Join(ErrCheckpointStoreFailed, err)
		}

		if lastEventTime.Valid {
			checkpoint.LastEventTime = lastEventTime.Time
		}

		checkpoints = append(checkpoints, &checkpoint)
	}

	if err = rows.Err(); err != nil {
		return nil, errors.Join(ErrCheckpointStoreFailed, err)
	}

	return checkpoints, nil
}

// SaveBatch 批量保存检查点
//
// 在事务中批量保存多个检查点，提高性能。
//
// 参数：
//   - ctx: 上下文
//   - checkpoints: 检查点列表
//
// 返回：
//   - error: 保存失败错误
func (s *SQLCheckpointStore) SaveBatch(ctx context.Context, checkpoints []*Checkpoint) error {
	if len(checkpoints) == 0 {
		return nil
	}

	// 开启事务，复用 Save 的 UPSERT 语义
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	for _, cp := range checkpoints {
		if cp == nil || !cp.IsValid() {
			continue
		}

		cp.UpdatedAt = time.Now()

		if _, err := sqlbuilder.New(tx).UpsertInto(s.tableName).
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
			return errors.Join(ErrCheckpointStoreFailed, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// Ensure SQLCheckpointStore implements ICheckpointStore
var _ ICheckpointStore = (*SQLCheckpointStore)(nil)
