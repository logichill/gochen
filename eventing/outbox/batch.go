package outbox

import (
	"context"
	"fmt"
	"strings"
	"time"

	sqlbuilder "gochen/storage/database/sql"

	"gochen/storage/database"
)

// IBatchRepository 批量操作接口
//
// 提供批量操作方法，减少数据库 IO，提升性能。
type IBatchRepository interface {
	// MarkAsPublishedBatch 批量标记为已发布
	MarkAsPublishedBatch(ctx context.Context, entryIDs []int64) error

	// MarkAsFailedBatch 批量标记为失败
	MarkAsFailedBatch(ctx context.Context, entries []FailedEntry) error

	// DeletePublishedBatch 批量删除已发布记录
	DeletePublishedBatch(ctx context.Context, entryIDs []int64) error
}

// FailedEntry 失败记录信息
type FailedEntry struct {
	ID          int64
	Error       string
	NextRetryAt string // 格式: "2006-01-02 15:04:05"
}

// BatchOperations SQL Repository 的批量操作扩展
//
// 为 SQLOutboxRepository 提供批量操作能力。
type BatchOperations struct {
	db database.IDatabase
}

// NewBatchOperations 创建批量操作实例
func NewBatchOperations(db database.IDatabase) IBatchRepository {
	return &BatchOperations{db: db}
}

// MarkAsPublishedBatch 批量标记为已发布
//
// 使用单个 SQL 语句更新多条记录：
//
//	UPDATE event_outbox
//	SET status = 'published', published_at = ?
//	WHERE id IN (1, 2, 3, ...)
func (b *BatchOperations) MarkAsPublishedBatch(ctx context.Context, entryIDs []int64) error {
	if len(entryIDs) == 0 {
		return nil
	}

	// 构造 IN 子句的占位符与参数
	placeholders := make([]string, len(entryIDs))
	args := make([]interface{}, len(entryIDs))
	for i, id := range entryIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	now := time.Now()
	cond := fmt.Sprintf("id IN (%s)", strings.Join(placeholders, ","))

	result, err := sqlbuilder.New(b.db).Update("event_outbox").
		Set("status", OutboxStatusPublished).
		Set("published_at", now).
		Where(cond, args...).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("batch mark as published: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows != int64(len(entryIDs)) {
		return fmt.Errorf("expected %d rows, but affected %d", len(entryIDs), rows)
	}

	return nil
}

// MarkAsFailedBatch 批量标记为失败
//
// 使用 CASE WHEN 语句批量更新不同的错误信息和重试时间。
func (b *BatchOperations) MarkAsFailedBatch(ctx context.Context, entries []FailedEntry) error {
	if len(entries) == 0 {
		return nil
	}

	// 构造 ID 列表与参数
	ids := make([]string, len(entries))
	idArgs := make([]interface{}, len(entries))

	// 构造 CASE WHEN 子句
	errorCases := make([]string, len(entries))
	retryCases := make([]string, len(entries))
	var errorArgs []interface{}
	var retryArgs []interface{}

	for i, entry := range entries {
		ids[i] = "?"
		idArgs[i] = entry.ID

		errorCases[i] = fmt.Sprintf("WHEN id = ? THEN ?")
		errorArgs = append(errorArgs, entry.ID, entry.Error)

		retryCases[i] = fmt.Sprintf("WHEN id = ? THEN ?")
		retryArgs = append(retryArgs, entry.ID, entry.NextRetryAt)
	}

	cond := fmt.Sprintf("id IN (%s)", strings.Join(ids, ","))
	errorExpr := fmt.Sprintf("last_error = CASE %s END", strings.Join(errorCases, " "))
	retryExpr := fmt.Sprintf("next_retry_at = CASE %s END", strings.Join(retryCases, " "))

	builder := sqlbuilder.New(b.db).Update("event_outbox").
		Set("status", OutboxStatusFailed).
		SetExpr("retry_count = retry_count + 1").
		SetExpr(errorExpr, errorArgs...).
		SetExpr(retryExpr, retryArgs...).
		Where(cond, idArgs...)

	_, err := builder.Exec(ctx)
	if err != nil {
		return fmt.Errorf("batch mark as failed: %w", err)
	}

	return nil
}

// DeletePublishedBatch 批量删除已发布记录
func (b *BatchOperations) DeletePublishedBatch(ctx context.Context, entryIDs []int64) error {
	if len(entryIDs) == 0 {
		return nil
	}

	placeholders := make([]string, len(entryIDs))
	args := make([]interface{}, len(entryIDs))
	for i, id := range entryIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	cond := fmt.Sprintf("id IN (%s)", strings.Join(placeholders, ","))

	_, err := sqlbuilder.New(b.db).DeleteFrom("event_outbox").
		Where(cond, args...).
		Where("status = ?", OutboxStatusPublished).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("batch delete published: %w", err)
	}

	return nil
}

// BatchPublisher 支持批量发布的发布器
//
// 对 ParallelPublisher 的封装，提供批量标记功能。
type BatchPublisher struct {
	*ParallelPublisher
	batchOps IBatchRepository
}

// NewBatchPublisher 创建批量发布器
func NewBatchPublisher(publisher *ParallelPublisher, batchOps IBatchRepository) *BatchPublisher {
	return &BatchPublisher{
		ParallelPublisher: publisher,
		batchOps:          batchOps,
	}
}

// processEntryBatch 批量处理 Outbox 记录
//
// 收集一批成功/失败的记录，然后批量更新状态。
func (p *BatchPublisher) processEntryBatch(ctx context.Context, entries []OutboxEntry) {
	var successIDs []int64
	var failedEntries []FailedEntry

	// 处理每条记录
	for _, entry := range entries {
		evt, err := entry.ToEvent()
		if err != nil {
			failedEntries = append(failedEntries, FailedEntry{
				ID:          entry.ID,
				Error:       err.Error(),
				NextRetryAt: entry.CalculateNextRetryTime(p.cfg.RetryInterval).Format("2006-01-02 15:04:05"),
			})
			continue
		}

		if err := p.bus.PublishEvent(ctx, &evt); err != nil {
			failedEntries = append(failedEntries, FailedEntry{
				ID:          entry.ID,
				Error:       err.Error(),
				NextRetryAt: entry.CalculateNextRetryTime(p.cfg.RetryInterval).Format("2006-01-02 15:04:05"),
			})
			continue
		}

		successIDs = append(successIDs, entry.ID)
	}

	// 批量更新状态
	if len(successIDs) > 0 {
		if err := p.batchOps.MarkAsPublishedBatch(ctx, successIDs); err != nil {
			p.log.Error(ctx, "batch mark as published failed")
		}
	}

	if len(failedEntries) > 0 {
		if err := p.batchOps.MarkAsFailedBatch(ctx, failedEntries); err != nil {
			p.log.Error(ctx, "batch mark as failed failed")
		}
	}
}
