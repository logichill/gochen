// Package audited 提供带审计能力的仓储抽象。
// 在 CRUD 仓储能力基础上扩展审计轨迹与软删/恢复能力，
// 面向“有审计/软删需求”的领域模型提供更语义化的导入路径：`domain/audited`。
package audited

import (
	"context"

	"gochen/domain/crud"
	"time"
)

// AuditRecord 审计记录
type AuditRecord struct {
	ID        int64          `json:"id"`
	EntityID  string         `json:"entity_id"`
	Operation string         `json:"operation"` // CREATE, UPDATE, DELETE, RESTORE
	Operator  string         `json:"operator"`
	Timestamp time.Time      `json:"timestamp"`
	Changes   map[string]any `json:"changes"` // 字段变更详情
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// IAuditedRepository 带审计语义的实体仓储接口。
//
// 设计要点：
//   - 仅在 CRUD 仓储基础上约束实体类型为 IAuditedEntity，确保实体具备审计/软删能力；
//   - 审计记录的写入应在仓储实现或服务层通过 IAuditStore 之类的装饰器完成，
//     避免在抽象接口上堆砌过多特定方法；
//   - 软删/恢复等操作可以由服务层基于 IEntity + ISoftDeletable 组合实现。
type IAuditedRepository[T IAuditedEntity[ID], ID comparable] interface {
	crud.IRepository[T, ID]
}

// IAuditStore 审计记录存储接口。
//
// 典型实现可以是独立的审计表/集合（如 article_audits），
// 由审计型仓储或服务在 Create/Update/Delete/SoftDelete/Restore 等操作后写入，
// 查询时则按实体维度拉取审计轨迹。
type IAuditStore interface {
	// SaveAuditRecord 保存一条审计记录。
	SaveAuditRecord(ctx context.Context, record AuditRecord) error

	// ListAuditRecordsByEntity 按实体 ID 查询审计记录（支持分页）。
	//
	// entityID 通常是实体主键的字符串形式，例如 fmt.Sprint(id)。
	ListAuditRecordsByEntity(ctx context.Context, entityID string, offset, limit int) ([]AuditRecord, error)
}
