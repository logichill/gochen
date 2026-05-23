package sqlstore

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gochen/db"
	"gochen/errors"
	"gochen/eventing"
	"gochen/logging"
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

// AppendEvents 向事件存储追加事件。
func (s *SQLEventStore[ID]) AppendEvents(ctx context.Context, aggregateID ID, events []eventing.IStorableEvent[ID], expectedVersion uint64) error {
	if len(events) == 0 {
		return nil
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		s.recordEventStoreError()
		return errors.NewCodeWithCause(errors.Database, "begin transaction failed", err)
	}
	defer tx.Rollback()
	if err := s.AppendEventsWithDB(ctx, tx, aggregateID, events, expectedVersion); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		s.recordEventStoreError()
		return errors.NewCodeWithCause(errors.Database, "commit transaction failed", err)
	}
	s.getLogger().Info(ctx, "events appended", logging.Any("aggregate_id", aggregateID), logging.Int("event_count", len(events)))
	return nil
}

func (s *SQLEventStore[ID]) AppendEventsWithDB(ctx context.Context, db db.IDatabase, aggregateID ID, events []eventing.IStorableEvent[ID], expectedVersion uint64) error {
	if len(events) == 0 {
		return nil
	}

	agg, err := s.codec.Encode(aggregateID)
	if err != nil {
		return errors.Wrap(err, errors.InvalidInput, "invalid aggregate id")
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
	currentVersion, err := s.getCurrentVersion(ctx, db, agg, aggregateType)
	if err != nil {
		s.recordEventStoreError()
		return errors.NewCodeWithCause(errors.Database, "query current version failed", err)
	}
	if currentVersion != expectedVersion {
		return errors.NewCode(errors.Concurrency,
			fmt.Sprintf("concurrency conflict: aggregate=%v, expected=%d, actual=%d", aggregateID, expectedVersion, currentVersion),
		).WithContext("aggregate_id", aggregateID).
			WithContext("expected_version", expectedVersion).
			WithContext("actual_version", currentVersion)
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
			return errors.NewCode(errors.InvalidInput, "mixed aggregate types in append batch").WithContext("event_id", evt.GetID()).WithContext("event_type", evt.GetType())
		}

		// 版本校验
		expectedEventVersion := expectedVersion + uint64(idx) + 1
		if evt.GetVersion() != expectedEventVersion {
			return errors.NewCode(errors.InvalidInput, fmt.Sprintf("event version mismatch: expected %d, got %d", expectedEventVersion, evt.GetVersion())).WithContext("event_id", evt.GetID()).WithContext("event_type", evt.GetType())
		}

		// 事件验证
		if err := evt.Validate(); err != nil {
			return errors.NewCode(errors.InvalidInput, fmt.Sprintf("event validation failed: %v", err)).WithContext("event_id", evt.GetID()).WithContext("event_type", evt.GetType())
		}

		// 序列化（CPU密集，但避免了数据库往返）
		payloadJSON, err := json.Marshal(evt.GetPayload())
		if err != nil {
			return errors.NewCodeWithCause(errors.Internal, "serialize payload failed", err).WithContext("event_id", evt.GetID()).WithContext("event_type", evt.GetType())
		}

		metadataJSON, err := json.Marshal(evt.GetMetadata())
		if err != nil {
			return errors.NewCodeWithCause(errors.Internal, "serialize metadata failed", err).WithContext("event_id", evt.GetID()).WithContext("event_type", evt.GetType())
		}

		// 使用 append 而非索引赋值，更健壮且语义更清晰
		prepared = append(prepared, preparedEvent{
			id:            evt.GetID(),
			typ:           evt.GetType(),
			aggregateType: evt.GetAggregateType(),
			version:       evt.GetVersion(),
			schemaVersion: evt.EventSchemaVersion(),
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
		err = executeRecoverableStatement(ctx, db, "append_single", func() error {
			_, err := db.Exec(ctx, insertSQL, p.id, p.typ, agg, p.aggregateType, p.version, p.schemaVersion, p.timestamp, p.payloadJSON, p.metadataJSON)
			return err
		})
		if err != nil {
			if isDuplicateKeyError(err) {
				idempotent, classified := s.classifyUniqueInsertError(ctx, db, aggregateID, agg, p, err)
				if idempotent {
					// 幂等性：相同事件已存在
					duration := time.Since(start)
					s.recordEventSaved(len(events), duration)
					return nil
				}
				return classified
			}
			s.recordEventStoreError()
			return errors.NewCodeWithCause(errors.Database, "insert event failed", err).WithContext("event_id", p.id).WithContext("event_type", p.typ)
		}
	} else {
		// 多个事件：使用批量INSERT
		placeholders := make([]string, len(prepared))
		args := make([]any, 0, len(prepared)*9)

		for i, p := range prepared {
			placeholders[i] = "(?, ?, ?, ?, ?, ?, ?, ?, ?)"
			args = append(args,
				p.id, p.typ, agg, p.aggregateType,
				p.version, p.schemaVersion, p.timestamp,
				p.payloadJSON, p.metadataJSON,
			)
		}

		batchSQL := fmt.Sprintf(
			"INSERT INTO %s (id, type, aggregate_id, aggregate_type, version, schema_version, timestamp, payload, metadata) VALUES %s",
			s.tableName,
			strings.Join(placeholders, ","),
		)

		err = executeRecoverableStatement(ctx, db, "append_batch", func() error {
			_, err := db.Exec(ctx, batchSQL, args...)
			return err
		})
		if err != nil {
			// 批量插入失败：可能是部分事件重复
			// 降级策略：回退到逐个插入以获得更好的错误处理
			if isDuplicateKeyError(err) {
				s.getLogger().Debug(ctx, "batch insert failed with duplicate key, falling back to individual inserts", logging.Int("event_count", len(prepared)))
				return s.appendEventsIndividually(ctx, db, aggregateID, agg, prepared, start)
			}
			s.recordEventStoreError()
			return errors.NewCodeWithCause(errors.Database, "batch insert events failed", err)
		}
	}

	duration := time.Since(start)
	s.recordEventSaved(len(events), duration)
	s.getLogger().Debug(ctx, "append batch done", logging.Any("aggregate_id", aggregateID), logging.Int("written", len(events)), logging.Int64("ms", duration.Milliseconds()))
	return nil
}

func (s *SQLEventStore[ID]) appendEventsIndividually(ctx context.Context, db db.IDatabase, aggregateID ID, agg any, prepared []preparedEvent, start time.Time) error {
	insertSQL := fmt.Sprintf(`INSERT INTO %s (id, type, aggregate_id, aggregate_type, version, schema_version, timestamp, payload, metadata) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, s.tableName)

	for _, p := range prepared {
		err := executeRecoverableStatement(ctx, db, "append_single", func() error {
			_, err := db.Exec(ctx, insertSQL, p.id, p.typ, agg, p.aggregateType, p.version, p.schemaVersion, p.timestamp, p.payloadJSON, p.metadataJSON)
			return err
		})
		if err != nil {
			if isDuplicateKeyError(err) {
				idempotent, classified := s.classifyUniqueInsertError(ctx, db, aggregateID, agg, p, err)
				if idempotent {
					continue // 跳过重复事件
				}
				return classified
			}
			s.recordEventStoreError()
			return errors.NewCodeWithCause(errors.Database, "insert event failed", err).WithContext("event_id", p.id).WithContext("event_type", p.typ)
		}
	}

	duration := time.Since(start)
	s.recordEventSaved(len(prepared), duration)
	return nil
}
