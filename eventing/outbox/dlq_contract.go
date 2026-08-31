package outbox

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"gochen/errors"
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

// DefaultOutboxDLQTable 是 DLQ 记录的默认表名。
const DefaultOutboxDLQTable = "event_outbox_dlq"

// TableName 返回 DLQ 默认使用的表名。
func (DLQEntry[ID]) TableName() string {
	return DefaultOutboxDLQTable
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
		return eventing.Event[ID]{}, errors.Wrap(err, errors.InvalidInput, "unmarshal event data failed")
	}
	return evt, nil
}

// IDLQRepository 定义持久 Outbox 发布失败记录的读写与重试能力。
//
// 该契约处理 publisher 在反序列化、升级或发布阶段耗尽重试的记录，并保留原 Outbox 状态关联。
// 消息已交给 handler 后的处理失败应写入 messaging/deadletter.ISink；该 sink 不具备本接口的
// claim 状态、持久重投和 Outbox 原子迁移语义。
type IDLQRepository[ID comparable] interface {
	// MoveToDLQ 把一条已失败的 Outbox 记录迁入 DLQ。
	MoveToDLQ(ctx context.Context, entry OutboxEntry[ID]) error

	// GetDLQEntries 读取一批最新的 DLQ 记录。
	GetDLQEntries(ctx context.Context, limit int) ([]DLQEntry[ID], error)

	// RetryFromDLQ 把一条 DLQ 记录重新投回 Outbox。
	//
	// 说明：
	// - 正常重投会插入一条 pending Outbox 记录并删除 DLQ 记录；
	// - 若 Outbox 已存在 failed/dead_lettered 记录，则恢复为 pending 并删除 DLQ 记录；
	// - 若 Outbox 已存在 pending/processing 记录，则返回 Conflict 并保留 DLQ 记录；
	// - 若 Outbox 已存在 published 记录，则仅删除 DLQ 记录。
	RetryFromDLQ(ctx context.Context, entryID int64) error

	// DeleteDLQEntry 删除一条 DLQ 记录。
	DeleteDLQEntry(ctx context.Context, entryID int64) error

	// GetDLQCount 返回当前 DLQ 中的记录数量。
	GetDLQCount(ctx context.Context) (int64, error)
}

// IAtomicDLQMover 定义在同一事务中标记失败并迁入 DLQ 的可选能力。
type IAtomicDLQMover[ID comparable] interface {
	MarkFailedAndMoveToDLQ(ctx context.Context, entry OutboxEntry[ID], claimToken string, errorMsg string) error
}
