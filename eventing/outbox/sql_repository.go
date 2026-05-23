// Package outbox 提供简化的 Outbox 仓储实现。
package outbox

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gochen/codec"
	"gochen/codec/idcodec"
	"gochen/errors"

	"gochen/db/sql/sqlbuilder"

	"gochen/db"
	"gochen/eventing"
	estore "gochen/eventing/store"
	"gochen/logging"
	"gochen/messaging"
)

const defaultClaimLease = 5 * time.Minute

// SimpleSQLOutboxRepository 提供一个简单直接的 SQL Outbox 仓储实现。
type SimpleSQLOutboxRepository[ID comparable] struct {
	db          db.IDatabase
	eventStore  IEventStoreWithDB[ID]
	tableName   string
	outboxTable string
	logger      logging.ILogger
	codec       codec.ICodec[ID, any]
	claimLease  time.Duration
}

// IEventStoreWithDB 定义同时支持普通追加和事务内追加的事件存储能力。
type IEventStoreWithDB[ID comparable] interface {
	// 从 eventing/store 包导入
	Init(ctx context.Context) error
	AppendEvents(ctx context.Context, aggregateID ID, events []eventing.IStorableEvent[ID], expectedVersion uint64) error
	AppendEventsWithDB(ctx context.Context, db db.IDatabase, aggregateID ID, events []eventing.IStorableEvent[ID], expectedVersion uint64) error
	LoadEvents(ctx context.Context, aggregateID ID, afterVersion uint64) ([]eventing.Event[ID], error)
}

// NewSimpleSQLOutboxRepository 为 `int64` 聚合 ID 创建一个简化 SQL Outbox 仓储。
func NewSimpleSQLOutboxRepository(db db.IDatabase, eventStore IEventStoreWithDB[int64], logger logging.ILogger) (*SimpleSQLOutboxRepository[int64], error) {
	return NewSimpleSQLOutboxRepositoryWithCodec[int64](db, eventStore, idcodec.NewInt64[int64](), logger)
}

// NewSimpleSQLOutboxRepositoryWithConfig 创建带 OutboxConfig 的 int64 SQL Outbox 仓储。
func NewSimpleSQLOutboxRepositoryWithConfig(db db.IDatabase, eventStore IEventStoreWithDB[int64], logger logging.ILogger, cfg OutboxConfig) (*SimpleSQLOutboxRepository[int64], error) {
	return NewSimpleSQLOutboxRepositoryWithCodecAndConfig[int64](db, eventStore, idcodec.NewInt64[int64](), logger, cfg)
}

// NewSimpleSQLOutboxRepositoryWithCodec 创建可自定义聚合 ID 编解码方式的 SQL Outbox 仓储。
func NewSimpleSQLOutboxRepositoryWithCodec[ID comparable](
	db db.IDatabase,
	eventStore IEventStoreWithDB[ID],
	idCodec codec.ICodec[ID, any],
	logger logging.ILogger,
) (*SimpleSQLOutboxRepository[ID], error) {
	return NewSimpleSQLOutboxRepositoryWithCodecAndConfig(db, eventStore, idCodec, logger, DefaultOutboxConfig())
}

// NewSimpleSQLOutboxRepositoryWithCodecAndConfig 创建带 OutboxConfig 的 SQL Outbox 仓储。
func NewSimpleSQLOutboxRepositoryWithCodecAndConfig[ID comparable](
	db db.IDatabase,
	eventStore IEventStoreWithDB[ID],
	idCodec codec.ICodec[ID, any],
	logger logging.ILogger,
	cfg OutboxConfig,
) (*SimpleSQLOutboxRepository[ID], error) {
	if logger == nil {
		logger = logging.ComponentLogger("eventing.outbox.repository")
	}
	if db == nil {
		return nil, errors.NewCode(errors.InvalidInput, "db cannot be nil")
	}
	if eventStore == nil {
		return nil, errors.NewCode(errors.InvalidInput, "eventStore cannot be nil")
	}
	if idCodec == nil {
		return nil, errors.NewCode(errors.InvalidInput, "codec cannot be nil")
	}
	cfg = normalizeOutboxConfig(cfg)
	return &SimpleSQLOutboxRepository[ID]{
		db:          db,
		eventStore:  eventStore,
		tableName:   "event_store",
		outboxTable: "event_outbox",
		logger:      logger,
		codec:       idCodec,
		claimLease:  cfg.ClaimLease,
	}, nil
}

// GetClaimLease 返回仓储实际用于 claim/renew 的租约时长。
func (r *SimpleSQLOutboxRepository[ID]) GetClaimLease() time.Duration {
	if r == nil || r.claimLease <= 0 {
		return defaultClaimLease
	}
	return r.claimLease
}

// SaveWithEvents 在同一事务内保存事件流和对应的 Outbox 记录。
func (r *SimpleSQLOutboxRepository[ID]) SaveWithEvents(ctx context.Context, aggregateID ID, events []eventing.Event[ID]) error {
	if len(events) == 0 {
		return nil
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		r.logger.Error(ctx, "failed to begin transaction", logging.Any("aggregate_id", aggregateID), logging.Error(err))
		return errors.Wrap(err, errors.Database, "begin transaction failed").
			WithContext("aggregate_id", aggregateID)
	}
	defer tx.Rollback()

	// 1. 保存事件到事件存储
	expectedVersion := r.calculateExpectedVersion(events)
	storable := estore.ToStorable(events)
	if err := r.eventStore.AppendEventsWithDB(ctx, tx, aggregateID, storable, expectedVersion); err != nil {
		r.logger.Warn(ctx, "failed to save events", logging.Any("aggregate_id", aggregateID), logging.Int("event_count", len(events)), logging.Error(err))
		// 保留事件存储的原始错误码语义（例如 Concurrency），仅补充上下文信息。
		var appErr *errors.AppError
		if errors.As(err, &appErr) && appErr != nil {
			return appErr.
				WithContext("aggregate_id", aggregateID).
				WithContext("event_count", len(events))
		}
		return errors.Wrap(err, errors.Database, "append events failed").
			WithContext("aggregate_id", aggregateID).
			WithContext("event_count", len(events))
	}

	// 2. 保存 Outbox 记录
	if err := r.saveOutboxEntries(ctx, tx, aggregateID, storable); err != nil {
		r.logger.Warn(ctx, "failed to save outbox entries", logging.Any("aggregate_id", aggregateID), logging.Int("event_count", len(events)), logging.Error(err))
		var appErr *errors.AppError
		if errors.As(err, &appErr) && appErr != nil {
			return appErr.
				WithContext("aggregate_id", aggregateID).
				WithContext("event_count", len(events))
		}
		return errors.Wrap(err, errors.Database, "save outbox entries failed").
			WithContext("aggregate_id", aggregateID).
			WithContext("event_count", len(events))
	}

	// 3. 提交事务
	if err := tx.Commit(); err != nil {
		r.logger.Error(ctx, "failed to commit transaction", logging.Any("aggregate_id", aggregateID), logging.Error(err))
		return errors.Wrap(err, errors.Database, "commit transaction failed").
			WithContext("aggregate_id", aggregateID)
	}

	r.logger.Info(ctx, "successfully saved events and outbox entries", logging.Any("aggregate_id", aggregateID), logging.Int("event_count", len(events)))
	return nil
}

// saveOutboxEntries 把事件批次逐条写入 Outbox 表。
func (r *SimpleSQLOutboxRepository[ID]) saveOutboxEntries(ctx context.Context, tx db.ITransaction, aggregateID ID, events []eventing.IStorableEvent[ID]) error {
	agg, err := r.codec.Encode(aggregateID)
	if err != nil {
		return errors.Wrap(err, errors.InvalidInput, "invalid aggregate id")
	}

	sq, err := sqlbuilder.New(tx)
	if err != nil {
		return errors.Wrap(err, errors.Internal, "failed to create sql builder")
	}

	for _, event := range events {
		eventData, err := r.serializeEvent(event)
		if err != nil {
			return errors.Wrap(err, errors.Internal, "serialize event failed").
				WithContext("event_id", event.GetID())
		}

		_, err = sq.InsertInto(r.outboxTable).
			Columns("aggregate_id", "aggregate_type", "event_id", "event_type", "event_data", "status", "claim_token", "created_at", "retry_count").
			Values(
				agg,
				event.GetAggregateType(),
				event.GetID(),
				event.GetType(),
				eventData,
				OutboxStatusPending,
				"",
				time.Now(),
				0,
			).Exec(ctx)
		if err != nil {
			return errors.Wrap(err, errors.Database, "insert outbox entry failed")
		}
	}

	return nil
}

// ClaimPendingEntries 原子 claim 当前可发布或到期可重试的 Outbox 记录。
func (r *SimpleSQLOutboxRepository[ID]) ClaimPendingEntries(ctx context.Context, limit int) ([]OutboxEntry[ID], error) {
	if limit <= 0 {
		return nil, nil
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, errors.Wrap(err, errors.Database, "begin transaction failed")
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	sq, err := sqlbuilder.New(tx)
	if err != nil {
		return nil, errors.Wrap(err, errors.Internal, "failed to create sql builder")
	}

	now := time.Now()
	claimToken, err := newClaimToken()
	if err != nil {
		return nil, err
	}
	leaseUntil := now.Add(r.claimLease)

	builder := sq.Select(
		"id", "aggregate_id", "aggregate_type", "event_id", "event_type", "event_data",
		"status", "claim_token", "created_at", "published_at", "retry_count", "last_error", "lease_until", "next_retry_at",
	).From(r.outboxTable).
		Where(claimableOutboxEntriesWhere(), claimableOutboxEntriesArgs(now)...).
		OrderBy(sqlbuilder.OrderAsc("created_at")).
		Limit(limit).
		ForUpdate().
		SkipLocked()

	rows, err := builder.Query(ctx)
	if err != nil {
		return nil, errors.Wrap(err, errors.Database, "query pending entries failed")
	}
	defer rows.Close()

	var entries []OutboxEntry[ID]
	for rows.Next() {
		var entry OutboxEntry[ID]
		var rawAggID any
		var publishedAt, leaseUntil, nextRetryAt sql.NullTime
		var lastError sql.NullString

		err := rows.Scan(
			&entry.ID, &rawAggID, &entry.AggregateType,
			&entry.EventID, &entry.EventType, &entry.EventData,
			&entry.Status, &entry.ClaimToken, &entry.CreatedAt, &publishedAt,
			&entry.RetryCount, &lastError, &leaseUntil, &nextRetryAt,
		)
		if err != nil {
			return nil, errors.Wrap(err, errors.Database, "scan entry failed")
		}
		typedAggID, err := r.codec.Decode(rawAggID)
		if err != nil {
			return nil, errors.Wrap(err, errors.InvalidInput, "failed to scan aggregate_id").
				WithContext("entry_id", entry.ID).
				WithContext("event_id", entry.EventID)
		}
		entry.AggregateID = typedAggID

		if publishedAt.Valid {
			entry.PublishedAt = &publishedAt.Time
		}
		if lastError.Valid {
			entry.LastError = lastError.String
		}
		if leaseUntil.Valid {
			entry.LeaseUntil = &leaseUntil.Time
		}
		if nextRetryAt.Valid {
			entry.NextRetryAt = &nextRetryAt.Time
		}

		entries = append(entries, entry)
	}

	rowsErr := rows.Err()
	if rowsErr != nil {
		return nil, rowsErr
	}

	if len(entries) == 0 {
		if err := tx.Commit(); err != nil {
			return nil, errors.Wrap(err, errors.Database, "commit transaction failed")
		}
		committed = true
		return entries, nil
	}

	result, err := sq.Update(r.outboxTable).
		Set("status", OutboxStatusProcessing).
		Set("claim_token", claimToken).
		Set("lease_until", leaseUntil).
		Set("next_retry_at", nil).
		Where(inIDsClause("id", len(entries)), entryIDsToArgs(entries)...).
		Where(claimableOutboxEntriesWhere(), claimableOutboxEntriesArgs(now)...).
		Exec(ctx)
	if err != nil {
		return nil, errors.Wrap(err, errors.Database, "claim pending entries failed")
	}

	if err := ensureRowsAffected(result, int64(len(entries)), "claim pending entries affected unexpected rows"); err != nil {
		return nil, err
	}

	for i := range entries {
		entries[i].Status = OutboxStatusProcessing
		entries[i].ClaimToken = claimToken
		entries[i].LeaseUntil = &leaseUntil
		entries[i].NextRetryAt = nil
	}

	if err := tx.Commit(); err != nil {
		return nil, errors.Wrap(err, errors.Database, "commit transaction failed")
	}
	committed = true
	return entries, nil
}

// MarkAsPublished 把指定 Outbox 记录更新为已发布状态。
func (r *SimpleSQLOutboxRepository[ID]) MarkAsPublished(ctx context.Context, entryID int64, claimToken string) error {
	sq, err := sqlbuilder.New(r.db)
	if err != nil {
		return errors.Wrap(err, errors.Internal, "failed to create sql builder")
	}

	result, err := sq.Update(r.outboxTable).
		Set("status", OutboxStatusPublished).
		Set("claim_token", "").
		Set("lease_until", nil).
		Set("published_at", time.Now()).
		Set("next_retry_at", nil).
		Where("id = ?", entryID).
		Where("claim_token = ?", claimToken).
		Where("status = ?", OutboxStatusProcessing).
		Exec(ctx)
	if err != nil {
		r.logger.Warn(ctx, "failed to mark as published", logging.Int64("entry_id", entryID), logging.Error(err))
		return errors.Wrap(err, errors.Database, "mark as published failed").
			WithContext("entry_id", entryID)
	}

	if err := ensureRowsAffected(result, 1, "outbox entry claim is no longer owned"); err != nil {
		return err.WithContext("entry_id", entryID)
	}
	return nil
}

// MarkAsFailed 记录失败原因并安排下次重试时间。
func (r *SimpleSQLOutboxRepository[ID]) MarkAsFailed(ctx context.Context, entryID int64, claimToken string, errorMsg string, nextRetryAt time.Time) error {
	sq, err := sqlbuilder.New(r.db)
	if err != nil {
		return errors.Wrap(err, errors.Internal, "failed to create sql builder")
	}

	result, err := sq.Update(r.outboxTable).
		Set("status", OutboxStatusFailed).
		Set("claim_token", "").
		Set("lease_until", nil).
		Set("last_error", errorMsg).
		SetIncrement("retry_count", 1).
		Set("next_retry_at", nextRetryAt).
		Where("id = ?", entryID).
		Where("claim_token = ?", claimToken).
		Where("status = ?", OutboxStatusProcessing).
		Exec(ctx)
	if err != nil {
		r.logger.Warn(ctx, "failed to mark as failed", logging.Int64("entry_id", entryID), logging.Error(err))
		return errors.Wrap(err, errors.Database, "mark as failed failed").
			WithContext("entry_id", entryID)
	}

	if err := ensureRowsAffected(result, 1, "outbox entry claim is no longer owned"); err != nil {
		return err.WithContext("entry_id", entryID)
	}
	return nil
}

// RenewClaim 延长指定 claim 的 lease，避免长时间处理过程中被其他 worker 重新 claim。
func (r *SimpleSQLOutboxRepository[ID]) RenewClaim(ctx context.Context, entryID int64, claimToken string) error {
	sq, err := sqlbuilder.New(r.db)
	if err != nil {
		return errors.Wrap(err, errors.Internal, "failed to create sql builder")
	}

	leaseUntil := time.Now().Add(r.claimLease)
	result, err := sq.Update(r.outboxTable).
		Set("lease_until", leaseUntil).
		Where("id = ?", entryID).
		Where("claim_token = ?", claimToken).
		Where("status = ?", OutboxStatusProcessing).
		Exec(ctx)
	if err != nil {
		r.logger.Warn(ctx, "failed to renew outbox claim", logging.Int64("entry_id", entryID), logging.Error(err))
		return errors.Wrap(err, errors.Database, "renew outbox claim failed").
			WithContext("entry_id", entryID)
	}

	if err := ensureRowsAffected(result, 1, "outbox entry claim is no longer owned"); err != nil {
		return err.WithContext("entry_id", entryID)
	}
	return nil
}

// DeletePublished 删除发布时间早于阈值的已发布记录。
func (r *SimpleSQLOutboxRepository[ID]) DeletePublished(ctx context.Context, olderThan time.Time) error {
	sq, err := sqlbuilder.New(r.db)
	if err != nil {
		return errors.Wrap(err, errors.Internal, "failed to create sql builder")
	}

	builder := sq.DeleteFrom(r.outboxTable).
		Where("status = ?", OutboxStatusPublished).
		Where("published_at < ?", olderThan)

	result, err := builder.Exec(ctx)
	if err != nil {
		return errors.Wrap(err, errors.Database, "delete published failed").
			WithContext("older_than", olderThan)
	}

	if rowsAffected, _ := result.RowsAffected(); rowsAffected > 0 {
		r.logger.Info(ctx, "cleaned up published outbox entries", logging.Int64("deleted", rowsAffected))
	}
	return nil
}

// calculateExpectedVersion 根据事件批次首条事件的版本推导 expectedVersion。
func (r *SimpleSQLOutboxRepository[ID]) calculateExpectedVersion(events []eventing.Event[ID]) uint64 {
	if len(events) == 0 {
		return 0
	}
	return events[0].GetVersion() - 1
}

// serializeEvent 把事件编码为存入 Outbox 的 JSON 文本。
func (r *SimpleSQLOutboxRepository[ID]) serializeEvent(event eventing.IStorableEvent[ID]) (string, error) {
	data := map[string]any{
		"id":             event.GetID(),
		"type":           event.GetType(),
		"aggregate_id":   event.GetAggregateID(),
		"aggregate_type": event.GetAggregateType(),
		"version":        event.GetVersion(),
		"schema_version": event.EventSchemaVersion(),
		"timestamp":      event.GetTimestamp(),
		"payload":        messaging.PayloadValue(event.GetPayload()),
		"metadata":       event.GetMetadata(),
	}

	bytes, err := json.Marshal(data)
	if err != nil {
		return "", errors.Wrap(err, errors.Internal, "marshal event failed")
	}

	return string(bytes), nil
}

func claimableOutboxEntriesWhere() string {
	return "((status = ?) OR (status = ? AND (next_retry_at IS NULL OR next_retry_at <= ?)) OR (status = ? AND lease_until <= ?))"
}

func claimableOutboxEntriesArgs(now time.Time) []any {
	return []any{OutboxStatusPending, OutboxStatusFailed, now, OutboxStatusProcessing, now}
}

func entryIDsToArgs[ID comparable](entries []OutboxEntry[ID]) []any {
	args := make([]any, 0, len(entries))
	for _, entry := range entries {
		args = append(args, entry.ID)
	}
	return args
}

func int64SliceToArgs(ids []int64) []any {
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	return args
}

func inIDsClause(column string, count int) string {
	placeholders := make([]string, count)
	for i := 0; i < count; i++ {
		placeholders[i] = "?"
	}
	return fmt.Sprintf("%s IN (%s)", column, strings.Join(placeholders, ","))
}

func newClaimToken() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", errors.Wrap(err, errors.Internal, "generate outbox claim token failed")
	}
	return fmt.Sprintf("%x", raw[:]), nil
}
