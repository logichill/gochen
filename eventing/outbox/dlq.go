package outbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"gochen/data/db"
	"gochen/eventing"
)

// DLQEntry 死信队列记录
//
// 当 Outbox 记录多次重试失败后，会被移动到 DLQ 中。
type DLQEntry struct {
	ID              int64     `json:"id" gorm:"primaryKey;autoIncrement"`
	OriginalEntryID int64     `json:"original_entry_id" gorm:"index;not null"`
	AggregateID     int64     `json:"aggregate_id" gorm:"index;not null"`
	AggregateType   string    `json:"aggregate_type" gorm:"index;not null"`
	EventID         string    `json:"event_id" gorm:"uniqueIndex;not null"`
	EventType       string    `json:"event_type" gorm:"index;not null"`
	EventData       string    `json:"event_data" gorm:"type:text;not null"`
	FailureReason   string    `json:"failure_reason" gorm:"type:text"`
	RetryCount      int       `json:"retry_count" gorm:"not null"`
	MovedAt         time.Time `json:"moved_at" gorm:"index;not null"`
}

// TableName 返回 DLQ 表名
func (DLQEntry) TableName() string {
	return "event_outbox_dlq"
}

// ToEvent 将 DLQ 记录转换为事件
func (e *DLQEntry) ToEvent() (eventing.Event, error) {
	var evt eventing.Event
	if err := json.Unmarshal([]byte(e.EventData), &evt); err != nil {
		return eventing.Event{}, fmt.Errorf("unmarshal event data: %w", err)
	}
	return evt, nil
}

// IDLQRepository DLQ 仓储接口
//
// 管理死信队列记录的 CRUD 操作。
type IDLQRepository interface {
	// MoveToDLQ 将 Outbox 记录移动到 DLQ
	//
	// 通常在记录重试次数超过限制时调用。
	MoveToDLQ(ctx context.Context, entry OutboxEntry) error

	// GetDLQEntries 获取 DLQ 记录
	//
	// limit 指定最大返回数量。
	GetDLQEntries(ctx context.Context, limit int) ([]DLQEntry, error)

	// RetryFromDLQ 从 DLQ 重试记录
	//
	// 将 DLQ 记录重新插入到 Outbox，并删除 DLQ 记录。
	RetryFromDLQ(ctx context.Context, entryID int64) error

	// DeleteDLQEntry 删除 DLQ 记录
	DeleteDLQEntry(ctx context.Context, entryID int64) error

	// GetDLQCount 获取 DLQ 记录数量
	GetDLQCount(ctx context.Context) (int64, error)
}

// SQLDLQRepository DLQ 的 SQL 实现
type SQLDLQRepository struct {
	db          db.IDatabase
	outboxRepo  IOutboxRepository
	maxRetries  int
	autoCleanup bool
}

// NewSQLDLQRepository 创建 SQL DLQ 仓储
//
// maxRetries 指定 Outbox 记录重试多少次后移到 DLQ。
// autoCleanup 指定是否在移到 DLQ 后自动删除 Outbox 记录。
func NewSQLDLQRepository(
	db db.IDatabase,
	outboxRepo IOutboxRepository,
	maxRetries int,
	autoCleanup bool,
) IDLQRepository {
	if maxRetries <= 0 {
		maxRetries = 5 // 默认 5 次
	}
	return &SQLDLQRepository{
		db:          db,
		outboxRepo:  outboxRepo,
		maxRetries:  maxRetries,
		autoCleanup: autoCleanup,
	}
}

// MoveToDLQ 将 Outbox 记录移动到 DLQ
func (r *SQLDLQRepository) MoveToDLQ(ctx context.Context, entry OutboxEntry) error {
	// 构造 DLQ 记录
	dlqEntry := DLQEntry{
		OriginalEntryID: entry.ID,
		AggregateID:     entry.AggregateID,
		AggregateType:   entry.AggregateType,
		EventID:         entry.EventID,
		EventType:       entry.EventType,
		EventData:       entry.EventData,
		FailureReason:   entry.LastError,
		RetryCount:      entry.RetryCount,
		MovedAt:         time.Now(),
	}

	// 插入到 DLQ 表
	query := `
		INSERT INTO event_outbox_dlq (
			original_entry_id, aggregate_id, aggregate_type, event_id, event_type,
			event_data, failure_reason, retry_count, moved_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := r.db.Exec(ctx, query,
		dlqEntry.OriginalEntryID,
		dlqEntry.AggregateID,
		dlqEntry.AggregateType,
		dlqEntry.EventID,
		dlqEntry.EventType,
		dlqEntry.EventData,
		dlqEntry.FailureReason,
		dlqEntry.RetryCount,
		dlqEntry.MovedAt,
	)
	if err != nil {
		return fmt.Errorf("insert dlq entry: %w", err)
	}

	// 自动清理：删除原始 Outbox 记录
	if r.autoCleanup {
		deleteQuery := `DELETE FROM event_outbox WHERE id = ?`
		if _, err := r.db.Exec(ctx, deleteQuery, entry.ID); err != nil {
			return fmt.Errorf("delete outbox entry: %w", err)
		}
	}

	return nil
}

// GetDLQEntries 获取 DLQ 记录
func (r *SQLDLQRepository) GetDLQEntries(ctx context.Context, limit int) ([]DLQEntry, error) {
	if limit <= 0 {
		limit = 100
	}

	query := `
		SELECT id, original_entry_id, aggregate_id, aggregate_type, event_id, event_type,
		       event_data, failure_reason, retry_count, moved_at
		FROM event_outbox_dlq
		ORDER BY moved_at DESC
		LIMIT ?
	`

	rows, err := r.db.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("query dlq entries: %w", err)
	}
	defer rows.Close()

	var entries []DLQEntry
	for rows.Next() {
		var entry DLQEntry
		err := rows.Scan(
			&entry.ID,
			&entry.OriginalEntryID,
			&entry.AggregateID,
			&entry.AggregateType,
			&entry.EventID,
			&entry.EventType,
			&entry.EventData,
			&entry.FailureReason,
			&entry.RetryCount,
			&entry.MovedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan dlq entry: %w", err)
		}
		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return entries, nil
}

// RetryFromDLQ 从 DLQ 重试记录
func (r *SQLDLQRepository) RetryFromDLQ(ctx context.Context, entryID int64) error {
	// 1. 查询 DLQ 记录
	query := `
		SELECT id, original_entry_id, aggregate_id, aggregate_type, event_id, event_type,
		       event_data, failure_reason, retry_count, moved_at
		FROM event_outbox_dlq
		WHERE id = ?
	`

	var dlqEntry DLQEntry
	err := r.db.QueryRow(ctx, query, entryID).Scan(
		&dlqEntry.ID,
		&dlqEntry.OriginalEntryID,
		&dlqEntry.AggregateID,
		&dlqEntry.AggregateType,
		&dlqEntry.EventID,
		&dlqEntry.EventType,
		&dlqEntry.EventData,
		&dlqEntry.FailureReason,
		&dlqEntry.RetryCount,
		&dlqEntry.MovedAt,
	)
	if err == sql.ErrNoRows {
		return fmt.Errorf("dlq entry not found: %d", entryID)
	}
	if err != nil {
		return fmt.Errorf("query dlq entry: %w", err)
	}

	// 2. 重新插入到 Outbox（状态为 pending，重试计数清零）
	insertQuery := `
		INSERT INTO event_outbox (
			aggregate_id, aggregate_type, event_id, event_type, event_data,
			status, created_at, retry_count
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err = r.db.Exec(ctx, insertQuery,
		dlqEntry.AggregateID,
		dlqEntry.AggregateType,
		dlqEntry.EventID,
		dlqEntry.EventType,
		dlqEntry.EventData,
		OutboxStatusPending,
		time.Now(),
		0, // 重置重试计数
	)
	if err != nil {
		return fmt.Errorf("insert outbox entry: %w", err)
	}

	// 3. 删除 DLQ 记录
	deleteQuery := `DELETE FROM event_outbox_dlq WHERE id = ?`
	if _, err := r.db.Exec(ctx, deleteQuery, entryID); err != nil {
		return fmt.Errorf("delete dlq entry: %w", err)
	}

	return nil
}

// DeleteDLQEntry 删除 DLQ 记录
func (r *SQLDLQRepository) DeleteDLQEntry(ctx context.Context, entryID int64) error {
	query := `DELETE FROM event_outbox_dlq WHERE id = ?`
	result, err := r.db.Exec(ctx, query, entryID)
	if err != nil {
		return fmt.Errorf("delete dlq entry: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("dlq entry not found: %d", entryID)
	}

	return nil
}

// GetDLQCount 获取 DLQ 记录数量
func (r *SQLDLQRepository) GetDLQCount(ctx context.Context) (int64, error) {
	query := `SELECT COUNT(*) FROM event_outbox_dlq`
	var count int64
	err := r.db.QueryRow(ctx, query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("query dlq count: %w", err)
	}
	return count, nil
}

// ShouldMoveToDLQ 判断 Outbox 记录是否应该移到 DLQ
//
// 根据重试次数判断。
func (r *SQLDLQRepository) ShouldMoveToDLQ(entry OutboxEntry) bool {
	return entry.RetryCount >= r.maxRetries
}
