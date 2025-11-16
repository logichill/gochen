package repository

import (
	"context"
	"time"

	"gochen/domain/entity"
)

// AuditRecord 审计记录
type AuditRecord struct {
	ID        int64                  `json:"id"`
	EntityID  string                 `json:"entity_id"`
	Operation string                 `json:"operation"` // CREATE, UPDATE, DELETE, RESTORE
	Operator  string                 `json:"operator"`
	Timestamp time.Time              `json:"timestamp"`
	Changes   map[string]interface{} `json:"changes"` // 字段变更详情
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// IAuditedRepository 带审计的仓储接口
// 适用于操作记录型数据（订单、用户等）
type IAuditedRepository[T entity.Entity[ID], ID comparable] interface {
	IRepository[T, ID]

	// GetAuditTrail 获取实体的审计追踪
	GetAuditTrail(ctx context.Context, id ID) ([]AuditRecord, error)

	// ListDeleted 查询已软删除的实体
	ListDeleted(ctx context.Context, offset, limit int) ([]T, error)

	// Restore 恢复已删除的实体
	Restore(ctx context.Context, id ID, by string) error

	// SoftDelete 软删除实体
	SoftDelete(ctx context.Context, id ID, by string) error

	// PermanentDelete 永久删除实体（物理删除）
	PermanentDelete(ctx context.Context, id ID) error
}
