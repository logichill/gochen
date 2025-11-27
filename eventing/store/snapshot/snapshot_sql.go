package snapshot

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"gochen/logging"
	"gochen/storage/database"
)

// SQLStore 基于通用 database.IDatabase 的快照存储实现
//
// 语义说明：
// - 仅作为聚合重建的性能优化层，不改变事件存储的“真相”角色；
// - 每个 (aggregate_type, aggregate_id) 只保留一条最新快照；
// - SaveSnapshot 采用“UPDATE 若无则 INSERT”的幂等写入策略，兼容 MySQL/SQLite。
type SQLStore struct {
	db        database.IDatabase
	tableName string
}

// NewSQLStore 创建 SQL 快照存储
//
// tableName 为空时默认使用 "event_snapshots"。
func NewSQLStore(db database.IDatabase, tableName string) *SQLStore {
	if tableName == "" {
		tableName = "event_snapshots"
	}
	return &SQLStore{db: db, tableName: tableName}
}

// SaveSnapshot 保存或更新聚合快照
func (s *SQLStore) SaveSnapshot(ctx context.Context, snapshot Snapshot) error {
	if s.db == nil {
		return fmt.Errorf("snapshot SQLStore database is nil")
	}

	// 确保时间有效
	ts := snapshot.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}

	// 元数据序列化为 JSON（可为空）
	var metaJSON *string
	if len(snapshot.Metadata) > 0 {
		b, err := json.Marshal(snapshot.Metadata)
		if err != nil {
			return fmt.Errorf("serialize snapshot metadata failed: %w", err)
		}
		str := string(b)
		metaJSON = &str
	}

	// 先尝试 UPDATE，存在则更新
	updateSQL := fmt.Sprintf(`UPDATE %s
		SET version = ?, data = ?, timestamp = ?, metadata = ?
		WHERE aggregate_type = ? AND aggregate_id = ?`, s.tableName)
	res, err := s.db.Exec(ctx, updateSQL,
		snapshot.Version,
		snapshot.Data,
		ts,
		metaJSON,
		snapshot.AggregateType,
		snapshot.AggregateID,
	)
	if err != nil {
		return fmt.Errorf("update snapshot failed: %w", err)
	}
	if rows, errRA := res.RowsAffected(); errRA == nil && rows > 0 {
		snapshotLogger().Debug(ctx, "[SQLSnapshotStore] 更新快照",
			logging.Int64("aggregate_id", snapshot.AggregateID),
			logging.String("aggregate_type", snapshot.AggregateType),
			logging.Any("version", snapshot.Version),
		)
		return nil
	}

	// 不存在则 INSERT
	insertSQL := fmt.Sprintf(`INSERT INTO %s
		(aggregate_type, aggregate_id, version, data, timestamp, metadata)
		VALUES (?, ?, ?, ?, ?, ?)`, s.tableName)
	if _, err := s.db.Exec(ctx, insertSQL,
		snapshot.AggregateType,
		snapshot.AggregateID,
		snapshot.Version,
		snapshot.Data,
		ts,
		metaJSON,
	); err != nil {
		return fmt.Errorf("insert snapshot failed: %w", err)
	}

	snapshotLogger().Debug(ctx, "[SQLSnapshotStore] 创建快照",
		logging.Int64("aggregate_id", snapshot.AggregateID),
		logging.String("aggregate_type", snapshot.AggregateType),
		logging.Any("version", snapshot.Version),
	)
	return nil
}

// GetSnapshot 获取指定聚合的最新快照
func (s *SQLStore) GetSnapshot(ctx context.Context, aggregateType string, aggregateID int64) (*Snapshot, error) {
	if s.db == nil {
		return nil, fmt.Errorf("snapshot SQLStore database is nil")
	}

	query := fmt.Sprintf(`SELECT aggregate_id, aggregate_type, version, data, timestamp, metadata
		FROM %s
		WHERE aggregate_type = ? AND aggregate_id = ?`, s.tableName)

	row := s.db.QueryRow(ctx, query, aggregateType, aggregateID)
	var snap Snapshot
	var versionInt int64
	var ts time.Time
	var metaStr sql.NullString

	if err := row.Scan(
		&snap.AggregateID,
		&snap.AggregateType,
		&versionInt,
		&snap.Data,
		&ts,
		&metaStr,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("snapshot not found for aggregate %d", aggregateID)
		}
		return nil, fmt.Errorf("query snapshot failed: %w", err)
	}

	snap.Version = uint64(versionInt)
	snap.Timestamp = ts

	if metaStr.Valid && metaStr.String != "" {
		var meta map[string]any
		if err := json.Unmarshal([]byte(metaStr.String), &meta); err == nil {
			snap.Metadata = meta
		}
	}

	return &snap, nil
}

// DeleteSnapshot 删除指定聚合的快照
func (s *SQLStore) DeleteSnapshot(ctx context.Context, aggregateType string, aggregateID int64) error {
	if s.db == nil {
		return fmt.Errorf("snapshot SQLStore database is nil")
	}

	query := fmt.Sprintf(`DELETE FROM %s WHERE aggregate_type = ? AND aggregate_id = ?`, s.tableName)
	if _, err := s.db.Exec(ctx, query, aggregateType, aggregateID); err != nil {
		return fmt.Errorf("delete snapshot failed: %w", err)
	}

	snapshotLogger().Debug(ctx, "[SQLSnapshotStore] 删除快照",
		logging.Int64("aggregate_id", aggregateID),
		logging.String("aggregate_type", aggregateType),
	)
	return nil
}

// GetSnapshots 获取快照列表（可按聚合类型和数量限制）
func (s *SQLStore) GetSnapshots(ctx context.Context, aggregateType string, limit int) ([]Snapshot, error) {
	if s.db == nil {
		return nil, fmt.Errorf("snapshot SQLStore database is nil")
	}

	base := fmt.Sprintf(`SELECT aggregate_id, aggregate_type, version, data, timestamp, metadata
		FROM %s`, s.tableName)
	var (
		args  []any
		where string
		lim   string
	)
	if aggregateType != "" {
		where = " WHERE aggregate_type = ?"
		args = append(args, aggregateType)
	}
	if limit > 0 {
		lim = " LIMIT ?"
		args = append(args, limit)
	}

	query := base + where + " ORDER BY aggregate_type, aggregate_id" + lim
	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list snapshots failed: %w", err)
	}
	defer rows.Close()

	var result []Snapshot
	for rows.Next() {
		var snap Snapshot
		var versionInt int64
		var ts time.Time
		var metaStr sql.NullString

		if err := rows.Scan(
			&snap.AggregateID,
			&snap.AggregateType,
			&versionInt,
			&snap.Data,
			&ts,
			&metaStr,
		); err != nil {
			return nil, fmt.Errorf("scan snapshot row failed: %w", err)
		}
		snap.Version = uint64(versionInt)
		snap.Timestamp = ts

		if metaStr.Valid && metaStr.String != "" {
			var meta map[string]any
			if err := json.Unmarshal([]byte(metaStr.String), &meta); err == nil {
				snap.Metadata = meta
			}
		}

		result = append(result, snap)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate snapshot rows failed: %w", err)
	}

	return result, nil
}

// CleanupSnapshots 清理过期快照
func (s *SQLStore) CleanupSnapshots(ctx context.Context, retentionPeriod time.Duration) error {
	if s.db == nil {
		return fmt.Errorf("snapshot SQLStore database is nil")
	}

	cutoff := time.Now().Add(-retentionPeriod)
	query := fmt.Sprintf(`DELETE FROM %s WHERE timestamp < ?`, s.tableName)
	res, err := s.db.Exec(ctx, query, cutoff)
	if err != nil {
		return fmt.Errorf("cleanup snapshots failed: %w", err)
	}
	if deleted, errRA := res.RowsAffected(); errRA == nil {
		snapshotLogger().Info(ctx, "[SQLSnapshotStore] 清理过期快照",
			logging.Int64("deleted_count", deleted),
		)
	}
	return nil
}

// 确保 SQLStore 实现 Store 接口
var _ Store = (*SQLStore)(nil)
