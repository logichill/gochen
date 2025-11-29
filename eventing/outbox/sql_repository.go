// Package outbox 提供简化的 Outbox 仓储实现
package outbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	sqlbuilder "gochen/data/db/sql"

	"gochen/data/db"
	"gochen/eventing"
	"gochen/logging"
)

// OutboxError 统一的 Outbox 错误类型
type OutboxError struct {
	Code    string
	Message string
	Cause   error
}

func (e *OutboxError) Error() string {
	if e.Cause != nil {
		return e.Message + ": " + e.Cause.Error()
	}
	return e.Message
}

func (e *OutboxError) Unwrap() error { return e.Cause }

func newOutboxError(code, msg string, cause error) error {
	return &OutboxError{Code: code, Message: msg, Cause: cause}
}

// SimpleSQLOutboxRepository 简化的 SQL Outbox 仓储实现
type SimpleSQLOutboxRepository struct {
	db          database.IDatabase
	eventStore  EventStoreWithDB
	tableName   string
	outboxTable string
	logger      logging.Logger
}

// EventStoreWithDB 支持数据库接口的事件存储接口
type EventStoreWithDB interface {
	// 从 eventing/store 包导入
	Init(ctx context.Context) error
	AppendEvents(ctx context.Context, aggregateID int64, events []eventing.IStorableEvent, expectedVersion uint64) error
	AppendEventsWithDB(ctx context.Context, db database.IDatabase, aggregateID int64, events []eventing.IStorableEvent, expectedVersion uint64) error
	LoadEvents(ctx context.Context, aggregateID int64, afterVersion uint64) ([]eventing.Event, error)
}

// NewSimpleSQLOutboxRepository 创建简化的 SQL Outbox 仓储
func NewSimpleSQLOutboxRepository(db database.IDatabase, eventStore EventStoreWithDB, logger logging.Logger) *SimpleSQLOutboxRepository {
	if logger == nil {
		logger = logging.GetLogger()
	}
	return &SimpleSQLOutboxRepository{
		db:          db,
		eventStore:  eventStore,
		tableName:   "event_store",
		outboxTable: "event_outbox",
		logger:      logger,
	}
}

// SaveWithEvents 在同一事务中保存聚合事件和 Outbox 记录
func (r *SimpleSQLOutboxRepository) SaveWithEvents(ctx context.Context, aggregateID int64, events []eventing.Event) error {
	if len(events) == 0 {
		return nil
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		r.logger.Error(ctx, "开始事务失败", logging.Int64("aggregate_id", aggregateID), logging.Error(err))
		return newOutboxError("TX_BEGIN_FAILED", "begin transaction failed", err)
	}
	defer tx.Rollback()

	// 1. 保存事件到事件存储
	expectedVersion := r.calculateExpectedVersion(events)
	storable := eventing.ToStorable(events)
	if err := r.eventStore.AppendEventsWithDB(ctx, tx, aggregateID, storable, expectedVersion); err != nil {
		r.logger.Warn(ctx, "保存事件失败", logging.Int64("aggregate_id", aggregateID), logging.Int("event_count", len(events)), logging.Error(err))
		return newOutboxError("APPEND_EVENTS_FAILED", "save events failed", err)
	}

	// 2. 保存 Outbox 记录
	if err := r.saveOutboxEntries(ctx, tx, aggregateID, storable); err != nil {
		r.logger.Warn(ctx, "保存 Outbox 记录失败", logging.Int64("aggregate_id", aggregateID), logging.Int("event_count", len(events)), logging.Error(err))
		return newOutboxError("OUTBOX_SAVE_FAILED", "save outbox entries failed", err)
	}

	// 3. 提交事务
	if err := tx.Commit(); err != nil {
		r.logger.Error(ctx, "提交事务失败", logging.Int64("aggregate_id", aggregateID), logging.Error(err))
		return newOutboxError("TX_COMMIT_FAILED", "commit transaction failed", err)
	}

	r.logger.Info(ctx, "成功保存事件和 Outbox 记录", logging.Int64("aggregate_id", aggregateID), logging.Int("event_count", len(events)))
	return nil
}

// saveOutboxEntries 保存 Outbox 记录
func (r *SimpleSQLOutboxRepository) saveOutboxEntries(ctx context.Context, tx database.ITransaction, aggregateID int64, events []eventing.IStorableEvent) error {
	for _, event := range events {
		eventData, err := r.serializeEvent(event)
		if err != nil {
			return fmt.Errorf("serialize event %s failed: %w", event.GetID(), err)
		}

		_, err = sqlbuilder.New(tx).InsertInto(r.outboxTable).
			Columns("aggregate_id", "aggregate_type", "event_id", "event_type", "event_data", "status", "created_at", "retry_count").
			Values(
				aggregateID,
				event.GetAggregateType(),
				event.GetID(),
				event.GetType(),
				eventData,
				OutboxStatusPending,
				time.Now(),
				0,
			).Exec(ctx)
		if err != nil {
			return fmt.Errorf("insert outbox entry failed: %w", err)
		}
	}

	return nil
}

// GetPendingEntries 获取待发布的记录
func (r *SimpleSQLOutboxRepository) GetPendingEntries(ctx context.Context, limit int) ([]OutboxEntry, error) {
	builder := sqlbuilder.New(r.db).Select(
		"id", "aggregate_id", "aggregate_type", "event_id", "event_type", "event_data",
		"status", "created_at", "published_at", "retry_count", "last_error", "next_retry_at",
	).From(r.outboxTable).
		Where("status = ?", OutboxStatusPending).
		Or("status = ? AND (next_retry_at IS NULL OR next_retry_at <= ?)", OutboxStatusFailed, time.Now()).
		OrderBy("created_at ASC").
		Limit(limit)

	rows, err := builder.Query(ctx)
	if err != nil {
		return nil, fmt.Errorf("query pending entries failed: %w", err)
	}
	defer rows.Close()

	var entries []OutboxEntry
	for rows.Next() {
		var entry OutboxEntry
		var publishedAt, nextRetryAt sql.NullTime
		var lastError sql.NullString

		err := rows.Scan(
			&entry.ID, &entry.AggregateID, &entry.AggregateType,
			&entry.EventID, &entry.EventType, &entry.EventData,
			&entry.Status, &entry.CreatedAt, &publishedAt,
			&entry.RetryCount, &lastError, &nextRetryAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan entry failed: %w", err)
		}

		if publishedAt.Valid {
			entry.PublishedAt = &publishedAt.Time
		}
		if lastError.Valid {
			entry.LastError = lastError.String
		}
		if nextRetryAt.Valid {
			entry.NextRetryAt = &nextRetryAt.Time
		}

		entries = append(entries, entry)
	}

	return entries, rows.Err()
}

// MarkAsPublished 标记为已发布
func (r *SimpleSQLOutboxRepository) MarkAsPublished(ctx context.Context, entryID int64) error {
	builder := sqlbuilder.New(r.db).Update(r.outboxTable).
		Set("status", OutboxStatusPublished).
		Set("published_at", time.Now()).
		Where("id = ?", entryID)

	_, err := builder.Exec(ctx)
	if err != nil {
		r.logger.Warn(ctx, "标记已发布失败", logging.Int64("entry_id", entryID), logging.Error(err))
		return newOutboxError("OUTBOX_PUBLISH_UPDATE_FAILED", "mark as published failed", err)
	}
	return nil
}

// MarkAsFailed 标记为失败
func (r *SimpleSQLOutboxRepository) MarkAsFailed(ctx context.Context, entryID int64, errorMsg string, nextRetryAt time.Time) error {
	builder := sqlbuilder.New(r.db).Update(r.outboxTable).
		Set("status", OutboxStatusFailed).
		Set("last_error", errorMsg).
		SetExpr("retry_count = retry_count + 1").
		Set("next_retry_at", nextRetryAt).
		Where("id = ?", entryID)

	_, err := builder.Exec(ctx)
	if err != nil {
		r.logger.Warn(ctx, "标记事件失败状态失败", logging.Int64("entry_id", entryID), logging.Error(err))
		return newOutboxError("OUTBOX_MARK_FAILED", "mark as failed failed", err)
	}
	return nil
}

// DeletePublished 删除已发布的记录
func (r *SimpleSQLOutboxRepository) DeletePublished(ctx context.Context, olderThan time.Time) error {
	builder := sqlbuilder.New(r.db).DeleteFrom(r.outboxTable).
		Where("status = ?", OutboxStatusPublished).
		Where("published_at < ?", olderThan)

	result, err := builder.Exec(ctx)
	if err != nil {
		return newOutboxError("OUTBOX_DELETE_FAILED", "delete published failed", err)
	}

	if rowsAffected, _ := result.RowsAffected(); rowsAffected > 0 {
		r.logger.Info(ctx, "清理已发布 Outbox 记录", logging.Int64("deleted", rowsAffected))
	}
	return nil
}

// EnsureTable 确保表存在
func (r *SimpleSQLOutboxRepository) EnsureTable(ctx context.Context) error {
	createSQL := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id BIGINT PRIMARY KEY AUTO_INCREMENT,
			aggregate_id BIGINT NOT NULL,
			aggregate_type VARCHAR(255) NOT NULL,
			event_id VARCHAR(255) NOT NULL UNIQUE,
			event_type VARCHAR(255) NOT NULL,
			event_data TEXT NOT NULL,
			status VARCHAR(50) NOT NULL DEFAULT 'pending',
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			published_at TIMESTAMP NULL,
			retry_count INT NOT NULL DEFAULT 0,
			last_error TEXT NULL,
			next_retry_at TIMESTAMP NULL,
			INDEX idx_status_retry (status, next_retry_at),
			INDEX idx_aggregate (aggregate_id, aggregate_type),
			INDEX idx_created_at (created_at)
		)
	`, r.outboxTable)

	_, err := r.db.Exec(ctx, createSQL)
	if err != nil {
		return newOutboxError("OUTBOX_CREATE_TABLE_FAILED", "create outbox table failed", err)
	}

	r.logger.Info(ctx, "Outbox 表已就绪")
	return nil
}

// calculateExpectedVersion 计算期望版本
func (r *SimpleSQLOutboxRepository) calculateExpectedVersion(events []eventing.Event) uint64 {
	if len(events) == 0 {
		return 0
	}
	return events[0].GetVersion() - 1
}

// serializeEvent 序列化事件
func (r *SimpleSQLOutboxRepository) serializeEvent(event eventing.IStorableEvent) (string, error) {
	data := map[string]any{
		"id":             event.GetID(),
		"type":           event.GetType(),
		"aggregate_id":   event.GetAggregateID(),
		"aggregate_type": event.GetAggregateType(),
		"version":        event.GetVersion(),
		"schema_version": event.GetSchemaVersion(),
		"timestamp":      event.GetTimestamp(),
		"payload":        event.GetPayload(),
		"metadata":       event.GetMetadata(),
	}

	bytes, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("marshal event failed: %w", err)
	}

	return string(bytes), nil
}
