package outbox

import (
	"context"
	"fmt"
	"time"

	"gochen/data/db"
	"gochen/data/db/dialect"
	"gochen/logging"
)

// CleanupPolicy 清理策略配置
//
// 定义如何清理已发布的 Outbox 记录。
type CleanupPolicy struct {
	// RetentionDays 保留已发布记录的天数
	//
	// 超过此天数的已发布记录将被删除或归档。
	// 默认：7 天
	RetentionDays int `json:"retention_days"`

	// BatchSize 每次清理的批次大小
	//
	// 避免一次性删除大量数据导致数据库压力。
	// 默认：1000
	BatchSize int `json:"batch_size"`

	// ArchiveEnabled 是否启用归档
	//
	// 如果启用，记录将被移动到归档表而非删除。
	// 默认：false
	ArchiveEnabled bool `json:"archive_enabled"`

	// ArchiveTable 归档表名
	//
	// 仅在 ArchiveEnabled 为 true 时有效。
	// 默认："event_outbox_archive"
	ArchiveTable string `json:"archive_table"`

	// DryRun 试运行模式
	//
	// 如果为 true，只统计不实际删除。
	// 默认：false
	DryRun bool `json:"dry_run"`
}

// DefaultCleanupPolicy 返回默认清理策略
func DefaultCleanupPolicy() CleanupPolicy {
	return CleanupPolicy{
		RetentionDays:  7,
		BatchSize:      1000,
		ArchiveEnabled: false,
		ArchiveTable:   "event_outbox_archive",
		DryRun:         false,
	}
}

// CleanupService 清理服务
//
// 定期清理已发布的 Outbox 记录，支持归档和批量删除。
//
// 使用示例：
//
//	policy := DefaultCleanupPolicy()
//	service := NewCleanupService(db, policy, logger)
//	result, err := service.Cleanup(ctx)
type CleanupService struct {
	db      database.IDatabase
	dialect dialect.Dialect
	policy  CleanupPolicy
	log     logging.Logger
}

// CleanupResult 清理结果
type CleanupResult struct {
	// DeletedCount 删除的记录数
	DeletedCount int64

	// ArchivedCount 归档的记录数
	ArchivedCount int64

	// Duration 清理耗时
	Duration time.Duration

	// Error 错误信息（如果有）
	Error error
}

// NewCleanupService 创建清理服务
func NewCleanupService(
	db database.IDatabase,
	policy CleanupPolicy,
	logger logging.Logger,
) *CleanupService {
	if logger == nil {
		logger = logging.GetLogger()
	}

	// 设置默认值
	if policy.RetentionDays <= 0 {
		policy.RetentionDays = 7
	}
	if policy.BatchSize <= 0 {
		policy.BatchSize = 1000
	}
	if policy.ArchiveTable == "" {
		policy.ArchiveTable = "event_outbox_archive"
	}

	return &CleanupService{
		db:      db,
		dialect: dialect.FromDatabase(db),
		policy:  policy,
		log:     logger,
	}
}

// Cleanup 执行清理操作
//
// 根据配置的策略清理或归档已发布的记录。
func (s *CleanupService) Cleanup(ctx context.Context) (*CleanupResult, error) {
	startTime := time.Now()
	result := &CleanupResult{}

	// 计算截止时间
	cutoffTime := time.Now().AddDate(0, 0, -s.policy.RetentionDays)

	s.log.Info(ctx, "cleanup started")

	var err error
	if s.policy.ArchiveEnabled {
		// 归档模式
		result.ArchivedCount, err = s.archiveOldRecords(ctx, cutoffTime)
	} else {
		// 删除模式
		result.DeletedCount, err = s.deleteOldRecords(ctx, cutoffTime)
	}

	result.Duration = time.Since(startTime)
	result.Error = err

	if err != nil {
		s.log.Error(ctx, "cleanup failed")
		return result, err
	}

	s.log.Info(ctx, "cleanup completed")

	return result, nil
}

// deleteOldRecords 删除旧记录
func (s *CleanupService) deleteOldRecords(ctx context.Context, olderThan time.Time) (int64, error) {
	if s.policy.DryRun {
		// 试运行：只统计
		return s.countOldRecords(ctx, olderThan)
	}

	var totalDeleted int64

	for {
		var (
			sqlQuery string
			args     []any
		)

		// 构造方言兼容的 DELETE 语句
		if s.dialect.SupportsDeleteLimit() {
			sqlQuery = `
				DELETE FROM event_outbox
				WHERE status = ? AND published_at < ?
				LIMIT ?
			`
			args = []any{OutboxStatusPublished, olderThan, s.policy.BatchSize}
		} else {
			// Postgres 等不支持 DELETE ... LIMIT 的方言：
			// 通过子查询按主键限制删除数量。
			sqlQuery = `
				DELETE FROM event_outbox
				WHERE id IN (
					SELECT id FROM event_outbox
					WHERE status = ? AND published_at < ?
					ORDER BY published_at
					LIMIT ?
				)
			`
			args = []any{OutboxStatusPublished, olderThan, s.policy.BatchSize}
		}

		rawResult, err := s.db.Exec(ctx, sqlQuery, args...)
		if err != nil {
			return totalDeleted, fmt.Errorf("delete old records: %w", err)
		}

		affected, err := rawResult.RowsAffected()
		if err != nil {
			return totalDeleted, fmt.Errorf("get rows affected: %w", err)
		}

		totalDeleted += affected

		// 如果删除的记录少于批次大小，说明已经删完
		if affected < int64(s.policy.BatchSize) {
			break
		}

		// 避免长时间占用数据库
		select {
		case <-ctx.Done():
			return totalDeleted, ctx.Err()
		case <-time.After(10 * time.Millisecond):
			// 短暂休眠
		}
	}

	return totalDeleted, nil
}

// archiveOldRecords 归档旧记录
func (s *CleanupService) archiveOldRecords(ctx context.Context, olderThan time.Time) (int64, error) {
	if s.policy.DryRun {
		// 试运行：只统计
		return s.countOldRecords(ctx, olderThan)
	}

	var totalArchived int64

	for {
		// 1. 将记录复制到归档表（INSERT ... SELECT ... LIMIT 属于通用 SQL，方言均支持）
		insertQuery := fmt.Sprintf(`
			INSERT INTO %s (
				id, aggregate_id, aggregate_type, event_id, event_type, event_data,
				status, created_at, published_at, retry_count, last_error, next_retry_at
			)
			SELECT 
				id, aggregate_id, aggregate_type, event_id, event_type, event_data,
				status, created_at, published_at, retry_count, last_error, next_retry_at
			FROM event_outbox
			WHERE status = ? AND published_at < ?
			LIMIT ?
		`, s.policy.ArchiveTable)

		result, err := s.db.Exec(ctx, insertQuery,
			OutboxStatusPublished,
			olderThan,
			s.policy.BatchSize,
		)
		if err != nil {
			return totalArchived, fmt.Errorf("archive records: %w", err)
		}

		affected, err := result.RowsAffected()
		if err != nil {
			return totalArchived, fmt.Errorf("get rows affected: %w", err)
		}

		if affected == 0 {
			break
		}

		// 2. 删除已归档的记录（DELETE LIMIT 由方言兼容）
		var deleteQuery string
		var deleteArgs []any

		if s.dialect.SupportsDeleteLimit() {
			deleteQuery = `
				DELETE FROM event_outbox
				WHERE status = ? AND published_at < ?
				LIMIT ?
			`
			deleteArgs = []any{OutboxStatusPublished, olderThan, affected}
		} else {
			deleteQuery = `
				DELETE FROM event_outbox
				WHERE id IN (
					SELECT id FROM event_outbox
					WHERE status = ? AND published_at < ?
					ORDER BY published_at
					LIMIT ?
				)
			`
			deleteArgs = []any{OutboxStatusPublished, olderThan, affected}
		}

		_, err = s.db.Exec(ctx, deleteQuery, deleteArgs...)
		if err != nil {
			return totalArchived, fmt.Errorf("delete archived records: %w", err)
		}

		totalArchived += affected

		if affected < int64(s.policy.BatchSize) {
			break
		}

		// 避免长时间占用数据库
		select {
		case <-ctx.Done():
			return totalArchived, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}

	return totalArchived, nil
}

// countOldRecords 统计旧记录数量
func (s *CleanupService) countOldRecords(ctx context.Context, olderThan time.Time) (int64, error) {
	query := `
		SELECT COUNT(*)
		FROM event_outbox
		WHERE status = ? AND published_at < ?
	`

	var count int64
	err := s.db.QueryRow(ctx, query, OutboxStatusPublished, olderThan).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count old records: %w", err)
	}

	return count, nil
}

// GetStatistics 获取 Outbox 统计信息
//
// 用于监控和告警。
func (s *CleanupService) GetStatistics(ctx context.Context) (*OutboxStatistics, error) {
	query := `
		SELECT 
			SUM(CASE WHEN status = 'pending' THEN 1 ELSE 0 END) as pending,
			SUM(CASE WHEN status = 'published' THEN 1 ELSE 0 END) as published,
			SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END) as failed,
			MIN(created_at) as oldest,
			MAX(created_at) as newest
		FROM event_outbox
	`

	var stats OutboxStatistics
	var oldest, newest *time.Time

	err := s.db.QueryRow(ctx, query).Scan(
		&stats.PendingCount,
		&stats.PublishedCount,
		&stats.FailedCount,
		&oldest,
		&newest,
	)
	if err != nil {
		return nil, fmt.Errorf("get statistics: %w", err)
	}

	if oldest != nil {
		stats.OldestCreatedAt = *oldest
	}
	if newest != nil {
		stats.NewestCreatedAt = *newest
	}

	return &stats, nil
}

// OutboxStatistics Outbox 统计信息
type OutboxStatistics struct {
	PendingCount    int64     `json:"pending_count"`
	PublishedCount  int64     `json:"published_count"`
	FailedCount     int64     `json:"failed_count"`
	OldestCreatedAt time.Time `json:"oldest_created_at"`
	NewestCreatedAt time.Time `json:"newest_created_at"`
}
