package outbox

import (
	"strings"
	"time"

	"gochen/db"
	"gochen/db/dialect"
	"gochen/errors"
	"gochen/logging"
)

// CleanupPolicy 清理策略配置。
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

const defaultCleanupBatchSize = 1000

// DefaultCleanupPolicy 返回一组适合日常运行的默认 Outbox 清理策略。
func DefaultCleanupPolicy() CleanupPolicy {
	return CleanupPolicy{
		RetentionDays:  7,
		BatchSize:      defaultCleanupBatchSize,
		ArchiveEnabled: false,
		ArchiveTable:   "event_outbox_archive",
		DryRun:         false,
	}
}

// CleanupService 负责批量删除或归档已发布的 Outbox 记录。
type CleanupService struct {
	db      db.IDatabase
	dialect dialect.Dialect
	policy  CleanupPolicy
	log     logging.ILogger
}

// CleanupResult 表示一次清理任务的执行结果。
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

// NewCleanupService 创建一个 Outbox 清理服务，并完成策略归一化与校验。
func NewCleanupService(
	db db.IDatabase,
	policy CleanupPolicy,
	logger logging.ILogger,
) (*CleanupService, error) {
	if logger == nil {
		logger = logging.ComponentLogger("eventing.outbox.cleanup")
	}

	// 设置默认值
	if policy.RetentionDays <= 0 {
		policy.RetentionDays = 7
	}
	if policy.BatchSize <= 0 {
		policy.BatchSize = defaultCleanupBatchSize
	}
	if policy.ArchiveTable == "" {
		policy.ArchiveTable = "event_outbox_archive"
	}

	// 验证归档表名，防止 SQL 注入
	if policy.ArchiveEnabled {
		if err := validateTableName(policy.ArchiveTable); err != nil {
			return nil, errors.Wrap(err, errors.InvalidInput, "invalid archive table name").
				WithContext("table", policy.ArchiveTable)
		}
	}

	return &CleanupService{
		db:      db,
		dialect: dialect.FromDatabase(db),
		policy:  policy,
		log:     logger,
	}, nil
}

// validateTableName 校验归档表名是否安全，避免拼接 SQL 时引入注入风险。
func validateTableName(name string) error {
	if name == "" {
		return errors.NewCode(errors.InvalidInput, "table name cannot be empty")
	}
	parts := strings.Split(name, ".")
	if len(parts) > 2 {
		return errors.NewCode(errors.InvalidInput, "table name can only be table or schema.table")
	}
	for _, part := range parts {
		if part == "" {
			return errors.NewCode(errors.InvalidInput, "table name segment cannot be empty")
		}
		for _, r := range part {
			if (r >= 'a' && r <= 'z') ||
				(r >= 'A' && r <= 'Z') ||
				(r >= '0' && r <= '9') ||
				r == '_' {
				continue
			}
			return errors.NewCode(errors.InvalidInput, "invalid character in table name").WithContext("char", string(r))
		}
	}
	return nil
}
