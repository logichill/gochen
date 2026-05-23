package snapshot

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gochen/codec"
	"gochen/codec/idcodec"
	"gochen/db"
	"gochen/db/dialect"
	"gochen/db/sql/safeident"
	gerrors "gochen/errors"
	"gochen/logging"
)

// SQLStore 基于通用 db.IDatabase 的快照存储实现。
//
// 语义说明：
// - 仅作为聚合重建的性能优化层，不改变事件存储的“真相”角色；
// - 每个 (aggregate_type, aggregate_id) 只保留一条最新快照；
// - SaveSnapshot 采用“UPDATE 若无则 INSERT”的幂等写入策略，兼容 MySQL/SQLite。
type SQLStore[ID comparable] struct {
	db        db.IDatabase
	tableName string
	codec     codec.ICodec[ID, any]
	tableErr  error
}

// NewSQLStore 为 `int64` 聚合 ID 创建一个 SQL 快照存储。
func NewSQLStore(db db.IDatabase, tableName string) *SQLStore[int64] {
	return NewSQLStoreWithCodec[int64](db, tableName, idcodec.NewInt64[int64]())
}

// NewSQLStoreWithCodec 创建可自定义聚合 ID 编解码方式的 SQL 快照存储。
func NewSQLStoreWithCodec[ID comparable](db db.IDatabase, tableName string, idCodec codec.ICodec[ID, any]) *SQLStore[ID] {
	normalizedTableName, err := normalizeSnapshotSQLTableName(db, tableName, "event_snapshots")
	return &SQLStore[ID]{db: db, tableName: normalizedTableName, codec: idCodec, tableErr: err}
}

// SaveSnapshot 保存或覆盖指定聚合的最新快照。
func (s *SQLStore[ID]) SaveSnapshot(ctx context.Context, snapshot Snapshot[ID]) error {
	if s.db == nil {
		return gerrors.NewCode(gerrors.InvalidInput, "snapshot SQLStore database is nil")
	}
	if s.codec == nil {
		return gerrors.NewCode(gerrors.InvalidInput, "snapshot SQLStore codec is nil")
	}
	if s.tableErr != nil {
		return s.tableErr
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
			return gerrors.Wrap(err, gerrors.InvalidInput, "serialize snapshot metadata failed")
		}
		str := string(b)
		metaJSON = &str
	}

	// 先尝试 UPDATE，存在则更新
	updateSQL := fmt.Sprintf(`UPDATE %s
		SET version = ?, data = ?, timestamp = ?, metadata = ?
		WHERE aggregate_type = ? AND aggregate_id = ?`, s.tableName)
	agg, err := s.codec.Encode(snapshot.AggregateID)
	if err != nil {
		return gerrors.Wrap(err, gerrors.InvalidInput, "invalid aggregate id")
	}
	res, err := s.db.Exec(ctx, updateSQL,
		snapshot.Version,
		snapshot.Data,
		ts,
		metaJSON,
		snapshot.AggregateType,
		agg,
	)
	if err != nil {
		return gerrors.Wrap(err, gerrors.Database, "update snapshot failed").
			WithContext("aggregate_type", snapshot.AggregateType).
			WithContext("aggregate_id", snapshot.AggregateID)
	}
	if rows, errRA := res.RowsAffected(); errRA == nil && rows > 0 {
		snapshotLogger().Debug(ctx, "snapshot updated",
			logging.Any("aggregate_id", snapshot.AggregateID),
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
		agg,
		snapshot.Version,
		snapshot.Data,
		ts,
		metaJSON,
	); err != nil {
		return gerrors.Wrap(err, gerrors.Database, "insert snapshot failed").
			WithContext("aggregate_type", snapshot.AggregateType).
			WithContext("aggregate_id", snapshot.AggregateID)
	}

	snapshotLogger().Debug(ctx, "snapshot created",
		logging.Any("aggregate_id", snapshot.AggregateID),
		logging.String("aggregate_type", snapshot.AggregateType),
		logging.Any("version", snapshot.Version),
	)
	return nil
}

// FindSnapshot 读取指定聚合当前保存的最新快照。
func (s *SQLStore[ID]) FindSnapshot(ctx context.Context, aggregateType string, aggregateID ID) (*Snapshot[ID], error) {
	if s.db == nil {
		return nil, gerrors.NewCode(gerrors.InvalidInput, "snapshot SQLStore database is nil")
	}
	if s.codec == nil {
		return nil, gerrors.NewCode(gerrors.InvalidInput, "snapshot SQLStore codec is nil")
	}
	if s.tableErr != nil {
		return nil, s.tableErr
	}

	query := fmt.Sprintf(`SELECT aggregate_id, aggregate_type, version, data, timestamp, metadata
		FROM %s
		WHERE aggregate_type = ? AND aggregate_id = ?`, s.tableName)

	agg, err := s.codec.Encode(aggregateID)
	if err != nil {
		return nil, gerrors.Wrap(err, gerrors.InvalidInput, "invalid aggregate id")
	}

	row := s.db.QueryRow(ctx, query, aggregateType, agg)
	var snap Snapshot[ID]
	var rawAggID any
	var versionInt int64
	var ts time.Time
	var metaStr sql.NullString

	if err := row.Scan(
		&rawAggID,
		&snap.AggregateType,
		&versionInt,
		&snap.Data,
		&ts,
		&metaStr,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, gerrors.NewCode(gerrors.NotFound, "snapshot not found").
				WithContext("aggregate_type", aggregateType).
				WithContext("aggregate_id", aggregateID)
		}
		return nil, gerrors.Wrap(err, gerrors.Database, "query snapshot failed").
			WithContext("aggregate_type", aggregateType).
			WithContext("aggregate_id", aggregateID)
	}

	typedAggID, err := s.codec.Decode(rawAggID)
	if err != nil {
		return nil, gerrors.Wrap(err, gerrors.InvalidInput, "failed to scan aggregate_id").
			WithContext("aggregate_type", aggregateType).
			WithContext("aggregate_id", aggregateID)
	}
	snap.AggregateID = typedAggID
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

// DeleteSnapshot 删除指定聚合对应的快照。
func (s *SQLStore[ID]) DeleteSnapshot(ctx context.Context, aggregateType string, aggregateID ID) error {
	if s.db == nil {
		return gerrors.NewCode(gerrors.InvalidInput, "snapshot SQLStore database is nil")
	}
	if s.codec == nil {
		return gerrors.NewCode(gerrors.InvalidInput, "snapshot SQLStore codec is nil")
	}
	if s.tableErr != nil {
		return s.tableErr
	}

	query := fmt.Sprintf(`DELETE FROM %s WHERE aggregate_type = ? AND aggregate_id = ?`, s.tableName)
	agg, err := s.codec.Encode(aggregateID)
	if err != nil {
		return gerrors.Wrap(err, gerrors.InvalidInput, "invalid aggregate id")
	}

	if _, err := s.db.Exec(ctx, query, aggregateType, agg); err != nil {
		return gerrors.Wrap(err, gerrors.Database, "delete snapshot failed").
			WithContext("aggregate_type", aggregateType).
			WithContext("aggregate_id", aggregateID)
	}

	snapshotLogger().Debug(ctx, "snapshot deleted",
		logging.Any("aggregate_id", aggregateID),
		logging.String("aggregate_type", aggregateType),
	)
	return nil
}

// ListSnapshots 列出快照记录，可按聚合类型和数量限制过滤。
func (s *SQLStore[ID]) ListSnapshots(ctx context.Context, aggregateType string, limit int) ([]Snapshot[ID], error) {
	if s.db == nil {
		return nil, gerrors.NewCode(gerrors.InvalidInput, "snapshot SQLStore database is nil")
	}
	if s.codec == nil {
		return nil, gerrors.NewCode(gerrors.InvalidInput, "snapshot SQLStore codec is nil")
	}
	if s.tableErr != nil {
		return nil, s.tableErr
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
		return nil, gerrors.Wrap(err, gerrors.Database, "list snapshots failed").
			WithContext("aggregate_type", aggregateType)
	}
	defer rows.Close()

	var result []Snapshot[ID]
	for rows.Next() {
		var snap Snapshot[ID]
		var rawAggID any
		var versionInt int64
		var ts time.Time
		var metaStr sql.NullString

		if err := rows.Scan(
			&rawAggID,
			&snap.AggregateType,
			&versionInt,
			&snap.Data,
			&ts,
			&metaStr,
		); err != nil {
			return nil, gerrors.Wrap(err, gerrors.Database, "scan snapshot row failed")
		}
		typedAggID, err := s.codec.Decode(rawAggID)
		if err != nil {
			return nil, gerrors.Wrap(err, gerrors.InvalidInput, "failed to scan aggregate_id").
				WithContext("aggregate_type", snap.AggregateType)
		}
		snap.AggregateID = typedAggID
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
		return nil, gerrors.Wrap(err, gerrors.Database, "iterate snapshot rows failed")
	}

	return result, nil
}

// CleanupSnapshots 删除超过保留期限的快照记录。
func (s *SQLStore[ID]) CleanupSnapshots(ctx context.Context, retentionPeriod time.Duration) error {
	if s.db == nil {
		return gerrors.NewCode(gerrors.InvalidInput, "snapshot SQLStore database is nil")
	}
	if s.tableErr != nil {
		return s.tableErr
	}
	if retentionPeriod <= 0 {
		return nil
	}

	cutoff := time.Now().Add(-retentionPeriod)
	query := fmt.Sprintf(`DELETE FROM %s WHERE timestamp < ?`, s.tableName)
	res, err := s.db.Exec(ctx, query, cutoff)
	if err != nil {
		return gerrors.Wrap(err, gerrors.Database, "cleanup snapshots failed")
	}
	if deleted, errRA := res.RowsAffected(); errRA == nil {
		snapshotLogger().Info(ctx, "expired snapshots cleaned up",
			logging.Int64("deleted_count", deleted),
		)
	}
	return nil
}

// 确保 SQLStore 实现 ISnapshotStore[int64] 接口。
var _ ISnapshotStore[int64] = (*SQLStore[int64])(nil)

func normalizeSnapshotSQLTableName(database db.IDatabase, tableName, fallback string) (string, error) {
	tableName = strings.TrimSpace(tableName)
	if tableName == "" {
		tableName = fallback
	}
	if !safeident.IsSafeIdentifier(tableName) {
		return "", gerrors.NewCode(gerrors.InvalidInput, fmt.Sprintf("invalid SQL table name: %s", tableName))
	}
	return dialect.FromDatabase(database).QuoteIdentifier(tableName), nil
}
