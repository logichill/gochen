package sql

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gochen/data/db"
	"gochen/eventing"
	"gochen/eventing/monitoring"
	log "gochen/logging"
)

// preparedEvent 预处理的事件数据（用于批量插入优化）
type preparedEvent struct {
	id            string
	typ           string
	aggregateType string
	version       uint64
	schemaVersion int
	timestamp     time.Time
	payloadJSON   string
	metadataJSON  string
}

func (s *SQLEventStore) AppendEvents(ctx context.Context, aggregateID int64, events []eventing.IStorableEvent[int64], expectedVersion uint64) error {
	if len(events) == 0 {
		return nil
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return eventing.NewStoreFailedError("begin transaction failed", err)
	}
	defer tx.Rollback()
	if err := s.AppendEventsWithDB(ctx, tx, aggregateID, events, expectedVersion); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return eventing.NewStoreFailedError("commit transaction failed", err)
	}
	log.GetLogger().Info(ctx, "events appended", log.Int64("aggregate_id", aggregateID), log.Int("event_count", len(events)))
	return nil
}

func (s *SQLEventStore) AppendEventsWithDB(ctx context.Context, db db.IDatabase, aggregateID int64, events []eventing.IStorableEvent[int64], expectedVersion uint64) error {
	if len(events) == 0 {
		return nil
	}

	// 第一步：确定聚合类型
	aggregateType := ""
	for _, evt := range events {
		if evt.GetAggregateType() != "" {
			aggregateType = evt.GetAggregateType()
			break
		}
	}

	// 第二步：版本检查（必须在事务内）
	currentVersion, err := s.getCurrentVersion(ctx, db, aggregateID, aggregateType)
	if err != nil {
		return eventing.NewStoreFailedError("query current version failed", err)
	}
	if currentVersion != expectedVersion {
		return eventing.NewConcurrencyError(aggregateID, expectedVersion, currentVersion)
	}

	// 第三步：性能优化 - 预先验证和序列化所有事件（减少数据库往返）
	// 这一步在版本检查后、插入前执行，避免无效写入
	prepared := make([]preparedEvent, 0, len(events))
	start := time.Now()

	for idx, evt := range events {

		// 聚合类型处理
		if evt.GetAggregateType() == "" {
			evt.SetAggregateType(aggregateType)
		} else if aggregateType == "" {
			aggregateType = evt.GetAggregateType()
		} else if evt.GetAggregateType() != aggregateType {
			return eventing.NewInvalidEventError(evt.GetID(), evt.GetType(), "mixed aggregate types in append batch")
		}

		// 版本校验
		expectedEventVersion := expectedVersion + uint64(idx) + 1
		if evt.GetVersion() != expectedEventVersion {
			return eventing.NewInvalidEventError(evt.GetID(), evt.GetType(), fmt.Sprintf("event version mismatch: expected %d, got %d", expectedEventVersion, evt.GetVersion()))
		}

		// 事件验证
		if err := evt.Validate(); err != nil {
			return eventing.NewInvalidEventErrorWithCause(evt.GetID(), evt.GetType(), "event validation failed", err)
		}

		// 序列化（CPU密集，但避免了数据库往返）
		payloadJSON, err := json.Marshal(evt.GetPayload())
		if err != nil {
			return &eventing.EventStoreError{Code: eventing.ErrCodeSerializePayload, Message: "serialize payload failed", Cause: err, EventID: evt.GetID(), EventType: evt.GetType()}
		}

		metadataJSON, err := json.Marshal(evt.GetMetadata())
		if err != nil {
			return &eventing.EventStoreError{Code: eventing.ErrCodeSerializeMetadata, Message: "serialize metadata failed", Cause: err, EventID: evt.GetID(), EventType: evt.GetType()}
		}

		// 使用 append 而非索引赋值，更健壮且语义更清晰
		prepared = append(prepared, preparedEvent{
			id:            evt.GetID(),
			typ:           evt.GetType(),
			aggregateType: evt.GetAggregateType(),
			version:       evt.GetVersion(),
			schemaVersion: evt.GetSchemaVersion(),
			timestamp:     evt.GetTimestamp(),
			payloadJSON:   string(payloadJSON),
			metadataJSON:  string(metadataJSON),
		})
	}

	// 第四步：性能优化 - 批量INSERT
	// 构建批量插入SQL: INSERT INTO table VALUES (?, ...), (?, ...), ...
	// 这将N次数据库往返降低到1次
	if len(prepared) == 1 {
		// 单个事件：使用简单INSERT（更易读的错误信息）
		p := prepared[0]
		insertSQL := fmt.Sprintf(`INSERT INTO %s (id, type, aggregate_id, aggregate_type, version, schema_version, timestamp, payload, metadata) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, s.tableName)
		_, err = db.Exec(ctx, insertSQL, p.id, p.typ, aggregateID, p.aggregateType, p.version, p.schemaVersion, p.timestamp, p.payloadJSON, p.metadataJSON)
		if err != nil {
			if isDuplicateKeyError(err) {
				if s.isSameEvent(ctx, db, p.id, p.version, aggregateID) {
					// 幂等性：相同事件已存在
					duration := time.Since(start)
					monitoring.GlobalMetrics().RecordEventSaved(len(events), duration)
					return nil
				}
				return eventing.NewEventAlreadyExistsError(p.id, aggregateID, p.version)
			}
			return eventing.NewStoreFailedErrorWithEvent("insert event failed", err, p.id, p.typ)
		}
	} else {
		// 多个事件：使用批量INSERT
		placeholders := make([]string, len(prepared))
		args := make([]interface{}, 0, len(prepared)*9)

		for i, p := range prepared {
			placeholders[i] = "(?, ?, ?, ?, ?, ?, ?, ?, ?)"
			args = append(args,
				p.id, p.typ, aggregateID, p.aggregateType,
				p.version, p.schemaVersion, p.timestamp,
				p.payloadJSON, p.metadataJSON,
			)
		}

		batchSQL := fmt.Sprintf(
			"INSERT INTO %s (id, type, aggregate_id, aggregate_type, version, schema_version, timestamp, payload, metadata) VALUES %s",
			s.tableName,
			strings.Join(placeholders, ","),
		)

		_, err = db.Exec(ctx, batchSQL, args...)
		if err != nil {
			// 批量插入失败：可能是部分事件重复
			// 降级策略：回退到逐个插入以获得更好的错误处理
			if isDuplicateKeyError(err) {
				log.GetLogger().Debug(ctx, "batch insert failed with duplicate key, falling back to individual inserts", log.Int("event_count", len(prepared)))
				return s.appendEventsIndividually(ctx, db, aggregateID, prepared, start)
			}
			return eventing.NewStoreFailedError("batch insert events failed", err)
		}
	}

	duration := time.Since(start)
	monitoring.GlobalMetrics().RecordEventSaved(len(events), duration)
	log.GetLogger().Debug(ctx, "append batch done", log.Int64("aggregate_id", aggregateID), log.Int("written", len(events)), log.Int64("ms", duration.Milliseconds()))
	return nil
}

// appendEventsIndividually 降级策略：逐个插入事件（仅在批量插入失败时使用）
func (s *SQLEventStore) appendEventsIndividually(ctx context.Context, db db.IDatabase, aggregateID int64, prepared []preparedEvent, start time.Time) error {
	insertSQL := fmt.Sprintf(`INSERT INTO %s (id, type, aggregate_id, aggregate_type, version, schema_version, timestamp, payload, metadata) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, s.tableName)

	for _, p := range prepared {
		_, err := db.Exec(ctx, insertSQL, p.id, p.typ, aggregateID, p.aggregateType, p.version, p.schemaVersion, p.timestamp, p.payloadJSON, p.metadataJSON)
		if err != nil {
			if isDuplicateKeyError(err) {
				// 检查幂等性
				if s.isSameEvent(ctx, db, p.id, p.version, aggregateID) {
					continue // 跳过重复事件
				}
				return eventing.NewEventAlreadyExistsError(p.id, aggregateID, p.version)
			}
			return eventing.NewStoreFailedErrorWithEvent("insert event failed", err, p.id, p.typ)
		}
	}

	duration := time.Since(start)
	monitoring.GlobalMetrics().RecordEventSaved(len(prepared), duration)
	return nil
}

func (s *SQLEventStore) getCurrentVersion(ctx context.Context, db db.IDatabase, aggregateID int64, aggregateType string) (uint64, error) {
	var current uint64
	row := db.QueryRow(ctx, fmt.Sprintf("SELECT COALESCE(MAX(version), 0) FROM %s WHERE aggregate_id = ? AND aggregate_type = ?", s.tableName), aggregateID, aggregateType)
	if err := row.Scan(&current); err != nil {
		return 0, err
	}
	return current, nil
}

func (s *SQLEventStore) isSameEvent(ctx context.Context, db db.IDatabase, eventID string, version uint64, aggregateID int64) bool {
	var existingVersion uint64
	var existingAggregateID int64
	row := db.QueryRow(ctx, fmt.Sprintf("SELECT aggregate_id, version FROM %s WHERE id = ?", s.tableName), eventID)
	return row.Scan(&existingAggregateID, &existingVersion) == nil && existingVersion == version && existingAggregateID == aggregateID
}

func isDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "Duplicate entry") || strings.Contains(msg, "duplicate key") || strings.Contains(msg, "UNIQUE constraint failed")
}
