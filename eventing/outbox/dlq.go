package outbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"gochen/codec"
	"gochen/codec/idcodec"
	"gochen/db"
	gerrors "gochen/errors"
	"gochen/eventing"
)

// DLQEntry 表示一条从 Outbox 迁移到死信队列的记录。
type DLQEntry[ID comparable] struct {
	ID              int64     `json:"id" gorm:"primaryKey;autoIncrement"`
	OriginalEntryID int64     `json:"original_entry_id" gorm:"index;not null"`
	AggregateID     ID        `json:"aggregate_id" gorm:"index;not null"`
	AggregateType   string    `json:"aggregate_type" gorm:"index;not null"`
	EventID         string    `json:"event_id" gorm:"uniqueIndex;not null"`
	EventType       string    `json:"event_type" gorm:"index;not null"`
	EventData       string    `json:"event_data" gorm:"type:text;not null"`
	FailureReason   string    `json:"failure_reason" gorm:"type:text"`
	RetryCount      int       `json:"retry_count" gorm:"not null"`
	MovedAt         time.Time `json:"moved_at" gorm:"index;not null"`
}

// TableName 返回 DLQ 默认使用的表名。
func (DLQEntry[ID]) TableName() string {
	return "event_outbox_dlq"
}

// ToEvent 将 DLQ 记录转换为事件。
//
// 说明：
// - 使用 json.Decoder + UseNumber() 保持数字精度。
func (e *DLQEntry[ID]) ToEvent() (eventing.Event[ID], error) {
	var evt eventing.Event[ID]
	decoder := json.NewDecoder(strings.NewReader(e.EventData))
	decoder.UseNumber()
	if err := decoder.Decode(&evt); err != nil {
		return eventing.Event[ID]{}, gerrors.Wrap(err, gerrors.InvalidInput, "unmarshal event data failed")
	}
	return evt, nil
}

// IDLQRepository 定义死信队列记录的读写与重试能力。
type IDLQRepository[ID comparable] interface {
	// MoveToDLQ 把一条 Outbox 记录迁入 DLQ。
	MoveToDLQ(ctx context.Context, entry OutboxEntry[ID]) error

	// GetDLQEntries 读取一批最新的 DLQ 记录。
	GetDLQEntries(ctx context.Context, limit int) ([]DLQEntry[ID], error)

	// RetryFromDLQ 把一条 DLQ 记录重新投回 Outbox。
	RetryFromDLQ(ctx context.Context, entryID int64) error

	// DeleteDLQEntry 删除一条 DLQ 记录。
	DeleteDLQEntry(ctx context.Context, entryID int64) error

	// GetDLQCount 返回当前 DLQ 中的记录数量。
	GetDLQCount(ctx context.Context) (int64, error)
}

// SQLDLQRepository DLQ 的 SQL 实现。
type SQLDLQRepository[ID comparable] struct {
	db          db.IDatabase
	outboxRepo  IOutboxRepository[ID]
	maxRetries  int
	autoCleanup bool
	codec       codec.ICodec[ID, any]
}

// NewSQLDLQRepository 为 `int64` 聚合 ID 创建一个 SQL DLQ 仓储实现。
func NewSQLDLQRepository(
	db db.IDatabase,
	outboxRepo IOutboxRepository[int64],
	maxRetries int,
	autoCleanup bool,
) (IDLQRepository[int64], error) {
	return NewSQLDLQRepositoryWithCodec[int64](db, outboxRepo, idcodec.NewInt64[int64](), maxRetries, autoCleanup)
}

// NewSQLDLQRepositoryWithCodec 创建可自定义聚合 ID 编解码方式的 SQL DLQ 仓储。
func NewSQLDLQRepositoryWithCodec[ID comparable](
	db db.IDatabase,
	outboxRepo IOutboxRepository[ID],
	idCodec codec.ICodec[ID, any],
	maxRetries int,
	autoCleanup bool,
) (IDLQRepository[ID], error) {
	if maxRetries <= 0 {
		maxRetries = 5 // 默认 5 次
	}
	if db == nil {
		return nil, gerrors.NewCode(gerrors.InvalidInput, "db cannot be nil")
	}
	if outboxRepo == nil {
		return nil, gerrors.NewCode(gerrors.InvalidInput, "outboxRepo cannot be nil")
	}
	if idCodec == nil {
		return nil, gerrors.NewCode(gerrors.InvalidInput, "codec cannot be nil")
	}
	return &SQLDLQRepository[ID]{
		db:          db,
		outboxRepo:  outboxRepo,
		maxRetries:  maxRetries,
		autoCleanup: autoCleanup,
		codec:       idCodec,
	}, nil
}

// MoveToDLQ 在事务里把 Outbox 记录插入 DLQ，并按配置决定是否删除原记录。
func (r *SQLDLQRepository[ID]) MoveToDLQ(ctx context.Context, entry OutboxEntry[ID]) error {
	// 构造 DLQ 记录
	dlqEntry := DLQEntry[ID]{
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

	// 使用事务保证 DLQ 与 Outbox 操作的原子性
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return gerrors.Wrap(err, gerrors.Database, "begin transaction for move to dlq failed")
	}
	defer tx.Rollback()

	// 插入到 DLQ 表
	query := `
		INSERT INTO event_outbox_dlq (
			original_entry_id, aggregate_id, aggregate_type, event_id, event_type,
			event_data, failure_reason, retry_count, moved_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	agg, err := r.codec.Encode(dlqEntry.AggregateID)
	if err != nil {
		return gerrors.Wrap(err, gerrors.InvalidInput, "invalid aggregate id")
	}

	_, err = tx.Exec(ctx, query,
		dlqEntry.OriginalEntryID,
		agg,
		dlqEntry.AggregateType,
		dlqEntry.EventID,
		dlqEntry.EventType,
		dlqEntry.EventData,
		dlqEntry.FailureReason,
		dlqEntry.RetryCount,
		dlqEntry.MovedAt,
	)
	if err != nil {
		return gerrors.Wrap(err, gerrors.Database, "insert dlq entry failed")
	}

	// 自动清理：删除原始 Outbox 记录
	if r.autoCleanup {
		deleteQuery := `DELETE FROM event_outbox WHERE id = ?`
		if _, err := tx.Exec(ctx, deleteQuery, entry.ID); err != nil {
			return gerrors.Wrap(err, gerrors.Database, "delete outbox entry failed").
				WithContext("outbox_entry_id", entry.ID)
		}
	}

	if err := tx.Commit(); err != nil {
		return gerrors.Wrap(err, gerrors.Database, "commit move to dlq transaction failed")
	}

	return nil
}

// GetDLQEntries 按迁入时间倒序读取一批 DLQ 记录。
func (r *SQLDLQRepository[ID]) GetDLQEntries(ctx context.Context, limit int) ([]DLQEntry[ID], error) {
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
		return nil, gerrors.Wrap(err, gerrors.Database, "query dlq entries failed")
	}
	defer rows.Close()

	var entries []DLQEntry[ID]
	for rows.Next() {
		var entry DLQEntry[ID]
		var rawAggID any
		err := rows.Scan(
			&entry.ID,
			&entry.OriginalEntryID,
			&rawAggID,
			&entry.AggregateType,
			&entry.EventID,
			&entry.EventType,
			&entry.EventData,
			&entry.FailureReason,
			&entry.RetryCount,
			&entry.MovedAt,
		)
		if err != nil {
			return nil, gerrors.Wrap(err, gerrors.Database, "scan dlq entry failed")
		}
		typedAggID, err := r.codec.Decode(rawAggID)
		if err != nil {
			return nil, gerrors.Wrap(err, gerrors.InvalidInput, "failed to scan aggregate_id").
				WithContext("dlq_entry_id", entry.ID)
		}
		entry.AggregateID = typedAggID
		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, gerrors.Wrap(err, gerrors.Database, "iterate dlq entries failed")
	}

	return entries, nil
}

// RetryFromDLQ 把指定 DLQ 记录重新插入 Outbox，并在成功后删除 DLQ 记录。
func (r *SQLDLQRepository[ID]) RetryFromDLQ(ctx context.Context, entryID int64) error {
	// 使用事务保证 Outbox 插入与 DLQ 删除的原子性
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return gerrors.Wrap(err, gerrors.Database, "begin transaction for retry from dlq failed")
	}
	defer tx.Rollback()

	// 1. 查询 DLQ 记录
	query := `
		SELECT id, original_entry_id, aggregate_id, aggregate_type, event_id, event_type,
		       event_data, failure_reason, retry_count, moved_at
		FROM event_outbox_dlq
		WHERE id = ?
	`

	var dlqEntry DLQEntry[ID]
	var rawAggID any
	err = tx.QueryRow(ctx, query, entryID).Scan(
		&dlqEntry.ID,
		&dlqEntry.OriginalEntryID,
		&rawAggID,
		&dlqEntry.AggregateType,
		&dlqEntry.EventID,
		&dlqEntry.EventType,
		&dlqEntry.EventData,
		&dlqEntry.FailureReason,
		&dlqEntry.RetryCount,
		&dlqEntry.MovedAt,
	)
	if err == sql.ErrNoRows {
		return gerrors.NewCode(gerrors.NotFound, "dlq entry not found").WithContext("dlq_entry_id", entryID)
	}
	if err != nil {
		return gerrors.Wrap(err, gerrors.Database, "query dlq entry failed").
			WithContext("dlq_entry_id", entryID)
	}

	typedAggID, err := r.codec.Decode(rawAggID)
	if err != nil {
		return gerrors.Wrap(err, gerrors.InvalidInput, "failed to scan aggregate_id").
			WithContext("dlq_entry_id", entryID)
	}
	dlqEntry.AggregateID = typedAggID

	// 2. 重新插入到 Outbox（状态为 pending，重试计数清零）
	insertQuery := `
		INSERT INTO event_outbox (
			aggregate_id, aggregate_type, event_id, event_type, event_data,
			status, claim_token, created_at, retry_count
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	agg, err := r.codec.Encode(dlqEntry.AggregateID)
	if err != nil {
		return gerrors.Wrap(err, gerrors.InvalidInput, "invalid aggregate id").
			WithContext("dlq_entry_id", entryID)
	}

	_, err = tx.Exec(ctx, insertQuery,
		agg,
		dlqEntry.AggregateType,
		dlqEntry.EventID,
		dlqEntry.EventType,
		dlqEntry.EventData,
		OutboxStatusPending,
		"",
		time.Now(),
		0, // 重置重试计数
	)
	if err != nil {
		return gerrors.Wrap(err, gerrors.Database, "insert outbox entry failed").
			WithContext("dlq_entry_id", entryID)
	}

	// 3. 删除 DLQ 记录
	deleteQuery := `DELETE FROM event_outbox_dlq WHERE id = ?`
	if _, err := tx.Exec(ctx, deleteQuery, entryID); err != nil {
		return gerrors.Wrap(err, gerrors.Database, "delete dlq entry failed").
			WithContext("dlq_entry_id", entryID)
	}

	if err := tx.Commit(); err != nil {
		return gerrors.Wrap(err, gerrors.Database, "commit retry from dlq transaction failed").
			WithContext("dlq_entry_id", entryID)
	}

	return nil
}

// DeleteDLQEntry 删除实体并同步到存储。
//
// 说明：
// - DeleteDLQEntry 删除 DLQ 记录。
func (r *SQLDLQRepository[ID]) DeleteDLQEntry(ctx context.Context, entryID int64) error {
	query := `DELETE FROM event_outbox_dlq WHERE id = ?`
	result, err := r.db.Exec(ctx, query, entryID)
	if err != nil {
		return gerrors.Wrap(err, gerrors.Database, "delete dlq entry failed").
			WithContext("dlq_entry_id", entryID)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return gerrors.Wrap(err, gerrors.Database, "get rows affected failed")
	}
	if rows == 0 {
		return gerrors.NewCode(gerrors.NotFound, "dlq entry not found").WithContext("dlq_entry_id", entryID)
	}

	return nil
}

// GetDLQCount 从存储中查询实体。
//
// 说明：
// - GetDLQCount 获取 DLQ 记录数量。
func (r *SQLDLQRepository[ID]) GetDLQCount(ctx context.Context) (int64, error) {
	query := `SELECT COUNT(*) FROM event_outbox_dlq`
	var count int64
	err := r.db.QueryRow(ctx, query).Scan(&count)
	if err != nil {
		return 0, gerrors.Wrap(err, gerrors.Database, "query dlq count failed")
	}
	return count, nil
}

// ShouldMoveToDLQ 判断Move到DLQ。
func (r *SQLDLQRepository[ID]) ShouldMoveToDLQ(entry OutboxEntry[ID]) bool {
	return entry.RetryCount >= r.maxRetries
}
