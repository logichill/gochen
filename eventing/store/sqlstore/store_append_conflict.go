package sqlstore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"gochen/db"
	"gochen/errors"
)

func (s *SQLEventStore[ID]) getCurrentVersion(ctx context.Context, db db.IDatabase, aggregateID any, aggregateType string) (uint64, error) {
	var current uint64
	row := db.QueryRow(ctx, fmt.Sprintf("SELECT COALESCE(MAX(version), 0) FROM %s WHERE aggregate_id = ? AND aggregate_type = ?", s.tableName), aggregateID, aggregateType)
	if err := row.Scan(&current); err != nil {
		return 0, err
	}
	return current, nil
}

// isSameEvent 判断Same事件。
func (s *SQLEventStore[ID]) isSameEvent(ctx context.Context, db db.IDatabase, eventID string, version uint64, aggregateID ID) bool {
	var existingVersion uint64
	var existingAggregateID any
	row := db.QueryRow(ctx, fmt.Sprintf("SELECT aggregate_id, version FROM %s WHERE id = ?", s.tableName), eventID)
	if row.Scan(&existingAggregateID, &existingVersion) != nil {
		return false
	}
	typed, err := s.codec.Decode(existingAggregateID)
	if err != nil {
		return false
	}
	return existingVersion == version && typed == aggregateID
}

// isDuplicateKeyError 判断DuplicateKey错误。
func isDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	// Best-effort cross-dialect detection (we only get an error string through db.IDatabase).
	// Precise driver-level detection (e.g. SQLSTATE 23505) should live in db/ dialect helpers.
	return strings.Contains(msg, "Duplicate entry") || // mysql
		strings.Contains(msg, "duplicate key") || // postgres/other
		strings.Contains(msg, "SQLSTATE 23505") || // postgres
		strings.Contains(msg, "violates unique constraint") || // postgres
		strings.Contains(msg, "UNIQUE constraint failed") // sqlite
}

func (s *SQLEventStore[ID]) classifyUniqueInsertError(
	ctx context.Context,
	db db.IDatabase,
	aggregateID ID,
	encodedAggregateID any,
	p preparedEvent,
	insertErr error,
) (bool, error) {
	if ctx == nil {
		return false, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if s == nil {
		return false, errors.NewCode(errors.InvalidInput, "event store is nil")
	}

	// 1) 首先按 event_id 判断：若同一 event_id 已存在且完全匹配，则视为幂等成功；
	//    若 event_id 已存在但不匹配，则必然是 Duplicate（id 被占用），不应误报 Concurrency。
	found, existingAgg, existingVer, err := s.getEventByID(ctx, db, p.id)
	if err != nil {
		// 分类失败时：保留“插入失败”的核心语义，并携带冲突排查错误。
		return false, errors.NewCodeWithCause(errors.Database, "insert event failed", insertErr).
			WithContext("event_id", p.id).
			WithContext("event_type", p.typ).
			WithContext("conflict_resolution_error", err.Error())
	}
	if found {
		// Decode 失败时也应当视为“event_id 已被占用”，避免误判为 Concurrency。
		if existingAgg == aggregateID && existingVer == p.version {
			return true, nil
		}
		return false, errors.NewCode(errors.Duplicate, fmt.Sprintf("event %s already exists", p.id)).
			WithContext("event_id", p.id).
			WithContext("event_type", p.typ).
			WithContext("aggregate_id", aggregateID).
			WithContext("version", p.version)
	}

	// 2) event_id 不冲突时，再判断 (aggregate_id, aggregate_type, version) 是否已存在：
	//    这通常意味着“并发写入同一版本”，应归类为 Concurrency 而非 Duplicate。
	existingEventID, ok, err := s.getEventIDByAggregateVersion(ctx, db, encodedAggregateID, p.aggregateType, p.version)
	if err != nil {
		return false, errors.NewCodeWithCause(errors.Database, "insert event failed", insertErr).
			WithContext("event_id", p.id).
			WithContext("event_type", p.typ).
			WithContext("conflict_resolution_error", err.Error())
	}
	if ok {
		return false, errors.NewCode(errors.Concurrency,
			fmt.Sprintf("concurrency conflict: aggregate=%v, version=%d already exists", aggregateID, p.version),
		).WithContext("aggregate_id", aggregateID).
			WithContext("aggregate_type", p.aggregateType).
			WithContext("version", p.version).
			WithContext("existing_event_id", existingEventID)
	}

	// 3) 兜底：仍然认为是 Duplicate（例如：表存在其他唯一约束）。
	return false, errors.NewCode(errors.Duplicate, fmt.Sprintf("event %s already exists for aggregate %v version %d", p.id, aggregateID, p.version)).
		WithContext("event_id", p.id).
		WithContext("event_type", p.typ).
		WithContext("aggregate_id", aggregateID).
		WithContext("aggregate_type", p.aggregateType).
		WithContext("version", p.version)
}

func (s *SQLEventStore[ID]) getEventByID(ctx context.Context, db db.IDatabase, eventID string) (bool, ID, uint64, error) {
	var zero ID
	if ctx == nil {
		return false, zero, 0, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if db == nil {
		return false, zero, 0, errors.NewCode(errors.InvalidInput, "db is nil")
	}
	if s == nil || s.codec == nil {
		return false, zero, 0, errors.NewCode(errors.InvalidInput, "codec is nil")
	}

	var existingVersion uint64
	var existingAggregateID any
	row := db.QueryRow(ctx, fmt.Sprintf("SELECT aggregate_id, version FROM %s WHERE id = ?", s.tableName), eventID)
	if err := row.Scan(&existingAggregateID, &existingVersion); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, zero, 0, nil
		}
		return false, zero, 0, err
	}

	typed, err := s.codec.Decode(existingAggregateID)
	if err != nil {
		// 事件存在但 ID 无法解码：仍视为 "found"，避免误判为并发冲突。
		return true, zero, existingVersion, nil
	}
	return true, typed, existingVersion, nil
}

func (s *SQLEventStore[ID]) getEventIDByAggregateVersion(ctx context.Context, db db.IDatabase, encodedAggregateID any, aggregateType string, version uint64) (string, bool, error) {
	if ctx == nil {
		return "", false, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if db == nil {
		return "", false, errors.NewCode(errors.InvalidInput, "db is nil")
	}

	var id string
	row := db.QueryRow(ctx, fmt.Sprintf("SELECT id FROM %s WHERE aggregate_id = ? AND aggregate_type = ? AND version = ? LIMIT 1", s.tableName), encodedAggregateID, aggregateType, version)
	if err := row.Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return id, true, nil
}
