package outbox

import (
	"context"
	"strings"
	"time"

	"gochen/db"
	"gochen/db/sql/sqlbuilder"
	"gochen/errors"
)

// IBatchRepository 批量操作接口。
//
// 提供批量操作方法，减少数据库 IO，提升性能。
type IBatchRepository interface {
	// MarkAsPublishedBatch 批量标记为已发布
	MarkAsPublishedBatch(ctx context.Context, entries []ClaimedEntry) error

	// MarkAsFailedBatch 批量标记为失败
	MarkAsFailedBatch(ctx context.Context, entries []FailedEntry) error

	// DeletePublishedBatch 批量删除已发布记录
	DeletePublishedBatch(ctx context.Context, entryIDs []int64) error
}

// FailedEntry 失败记录信息。
type FailedEntry struct {
	ClaimedEntry
	Error       string
	NextRetryAt time.Time
}

// ClaimedEntry 表示一条已被 claim 的 Outbox 记录。
type ClaimedEntry struct {
	ID         int64
	ClaimToken string
}

// BatchOperations 定义批量Operations。
type BatchOperations struct {
	db        db.IDatabase
	tableName string
}

// NewBatchOperations 创建批量Operations。
func NewBatchOperations(db db.IDatabase) IBatchRepository {
	return NewBatchOperationsWithTable(db, "")
}

// NewBatchOperationsWithTable 创建批量Operations并带表。
func NewBatchOperationsWithTable(db db.IDatabase, tableName string) IBatchRepository {
	if tableName == "" {
		tableName = "event_outbox"
	}
	return &BatchOperations{db: db, tableName: tableName}
}

// MarkAsPublishedBatch 批量标记为已发布。
//
// 说明：
// - 使用单个 SQL 语句更新多条记录：
// - UPDATE event_outbox
// - SET status = 'published', published_at = ?
// - WHERE id IN (1, 2, 3, ...)
func (b *BatchOperations) MarkAsPublishedBatch(ctx context.Context, entries []ClaimedEntry) error {
	if len(entries) == 0 {
		return nil
	}

	now := time.Now()

	sq, err := sqlbuilder.New(b.db)
	if err != nil {
		return errors.Wrap(err, errors.Internal, "failed to create sql builder")
	}

	result, err := sq.Update(b.tableName).
		Set("status", OutboxStatusPublished).
		Set("published_at", now).
		Set("claim_token", "").
		Set("lease_until", nil).
		Set("next_retry_at", nil).
		Where(claimedEntriesWhere(entries), claimedEntriesArgs(entries)...).
		Where("status = ?", OutboxStatusProcessing).
		Exec(ctx)
	if err != nil {
		return errors.Wrap(err, errors.Database, "batch mark as published failed")
	}

	if err := ensureRowsAffected(result, int64(len(entries)), "batch mark as published affected unexpected rows"); err != nil {
		return err
	}

	return nil
}

// MarkAsFailedBatch 批量标记为失败。
//
// 说明：
// - 使用 CASE WHEN 语句批量更新不同的错误信息和重试时间。
func (b *BatchOperations) MarkAsFailedBatch(ctx context.Context, entries []FailedEntry) error {
	if len(entries) == 0 {
		return nil
	}

	// 构造 CASE WHEN 子句
	errorCases := make([]sqlbuilder.CaseWhenEq, 0, len(entries))
	retryCases := make([]sqlbuilder.CaseWhenEq, 0, len(entries))

	for _, entry := range entries {
		errorCases = append(errorCases, sqlbuilder.CaseWhenEq{
			When: entry.ID,
			Then: entry.Error,
		})
		retryCases = append(retryCases, sqlbuilder.CaseWhenEq{
			When: entry.ID,
			Then: entry.NextRetryAt,
		})
	}

	tx, err := b.db.Begin(ctx)
	if err != nil {
		return errors.Wrap(err, errors.Database, "begin transaction for batch mark as failed failed")
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	sq, err := sqlbuilder.New(tx)
	if err != nil {
		return errors.Wrap(err, errors.Internal, "failed to create sql builder")
	}

	builder := sq.Update(b.tableName).
		Set("status", OutboxStatusFailed).
		Set("claim_token", "").
		Set("lease_until", nil).
		SetIncrement("retry_count", 1).
		SetCaseWhenEq("last_error", "id", errorCases).
		SetCaseWhenEq("next_retry_at", "id", retryCases).
		Where(claimedEntriesWhereFromFailed(entries), claimedEntriesArgsFromFailed(entries)...).
		Where("status = ?", OutboxStatusProcessing)

	result, err := builder.Exec(ctx)
	if err != nil {
		return errors.Wrap(err, errors.Database, "batch mark as failed failed")
	}

	if err := ensureRowsAffected(result, int64(len(entries)), "batch mark as failed affected unexpected rows"); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return errors.Wrap(err, errors.Database, "commit batch mark as failed failed")
	}
	committed = true

	return nil
}

func claimedEntriesWhere(entries []ClaimedEntry) string {
	parts := make([]string, len(entries))
	for i := range entries {
		parts[i] = "(id = ? AND claim_token = ?)"
	}
	return "(" + strings.Join(parts, " OR ") + ")"
}

func claimedEntriesArgs(entries []ClaimedEntry) []any {
	args := make([]any, 0, len(entries)*2)
	for _, entry := range entries {
		args = append(args, entry.ID, entry.ClaimToken)
	}
	return args
}

func claimedEntriesWhereFromFailed(entries []FailedEntry) string {
	claimed := make([]ClaimedEntry, 0, len(entries))
	for _, entry := range entries {
		claimed = append(claimed, entry.ClaimedEntry)
	}
	return claimedEntriesWhere(claimed)
}

func claimedEntriesArgsFromFailed(entries []FailedEntry) []any {
	claimed := make([]ClaimedEntry, 0, len(entries))
	for _, entry := range entries {
		claimed = append(claimed, entry.ClaimedEntry)
	}
	return claimedEntriesArgs(claimed)
}

// DeletePublishedBatch 删除对象。
//
// 说明：
// - DeletePublishedBatch 批量删除已发布记录。
func (b *BatchOperations) DeletePublishedBatch(ctx context.Context, entryIDs []int64) error {
	if len(entryIDs) == 0 {
		return nil
	}

	args := make([]any, len(entryIDs))
	for i, id := range entryIDs {
		args[i] = id
	}

	sq, err := sqlbuilder.New(b.db)
	if err != nil {
		return errors.Wrap(err, errors.Internal, "failed to create sql builder")
	}

	_, err = sq.DeleteFrom(b.tableName).
		Where(inIDsClause("id", len(entryIDs)), args...).
		Where("status = ?", OutboxStatusPublished).
		Exec(ctx)
	if err != nil {
		return errors.Wrap(err, errors.Database, "batch delete published failed")
	}

	return nil
}

// BatchPublisher 支持批量发布的发布器。
//
// 对 ParallelPublisher 的封装，提供批量标记功能。
type BatchPublisher[ID comparable] struct {
	*ParallelPublisher[ID]
	batchOps IBatchRepository
}

// NewBatchPublisher 创建批量Publisher。
func NewBatchPublisher[ID comparable](publisher *ParallelPublisher[ID], batchOps IBatchRepository) *BatchPublisher[ID] {
	return &BatchPublisher[ID]{
		ParallelPublisher: publisher,
		batchOps:          batchOps,
	}
}
