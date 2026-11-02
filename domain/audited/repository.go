// Package audited 提供带审计能力的仓储抽象。
// 在 CRUD 仓储能力基础上扩展审计轨迹与软删/恢复能力，
// 面向“有审计/软删需求”的领域模型提供更语义化的导入路径：`domain/audited`。
package audited

import (
	"context"
	"encoding/json"
	"gochen/domain/spec"

	"gochen/domain"
	"gochen/domain/crud"
	"time"
)

// AuditRecord 审计记录（D11）。动作是开放集合，取代封闭的操作枚举。
//
// Dimensions 保存写入时的完整维度事实，OperationID 关联同一次逻辑操作。
// AuditTrail 通过记录自身的快照解释条件，支持实体物理删除后的历史查询。
type AuditRecord struct {
	ID            int64             `json:"id"`
	ResourceKind  string            `json:"resource_kind,omitempty"`
	EntityID      string            `json:"entity_id" entity:"id"`
	Action        string            `json:"action"`
	Operator      string            `json:"operator"`
	Initiator     string            `json:"initiator,omitempty"`
	Reason        string            `json:"reason,omitempty"`
	CorrelationID string            `json:"correlation_id,omitempty"`
	OperationID   string            `json:"operation_id"`
	Dimensions    DimensionSnapshot `json:"dimensions"`
	Timestamp     time.Time         `json:"timestamp"`
	Changes       json.RawMessage   `json:"changes,omitempty"` // 变更前后，经 Projector
	Metadata      json.RawMessage   `json:"metadata,omitempty"`
}

// FieldChange 表示单个字段的变更。
type FieldChange struct {
	Field string `json:"field"`
	Old   any    `json:"old"`
	New   any    `json:"new"`
}

// IAuditedRepository 带审计语义的实体仓储接口。
//
// # 错误契约。
//
// 复用 crud.IRepository 的错误语义（见 domain/crud/repository.go）。
// 审计场景常见的 errors.Conflict 来源于实体状态机（如 SoftDelete/Restore）：
//   - 已删除实体再次删除：errors.Conflict（"entity already deleted"）
//   - 未删除实体恢复：errors.Conflict（"entity not deleted"）
//
// 设计要点：
//   - 仅在 CRUD 仓储基础上约束实体类型为 IAuditedEntity，确保实体具备审计/软删能力；
//   - Application 通过独立 IAuditStore 协调业务与审计写入，使用同一事务；
//   - 查询、恢复等可选能力由仓储本体提供，组合根显式装配；
//   - 软删/恢复等操作可以由服务层基于 IEntity + domain.ISoftDeletable 组合实现。
type IAuditedRepository[T IAuditedEntity[ID], ID comparable] interface {
	crud.IRepository[T, ID]
}

// IAuditStore 审计记录存储接口。
//
// 典型实现可以是独立的审计表/集合（如 article_audits），
// 由审计型仓储或服务在 Create/Update/Delete/SoftDelete/Restore 等操作后写入，
// 查询时则按实体维度拉取审计轨迹。
//
// # 错误契约。
//
//   - SaveAuditRecord: 参数校验失败（entityID/action/operator 为空等）返回 errors.InvalidInput；
//     其他为基础设施错误。审计写入失败是否应导致业务写失败取决于应用层策略（audited app 为 fail-fast）
type IAuditStore interface {
	// SaveAuditRecord 保存一条审计记录，并返回生成的审计记录 ID。
	//
	// 约定：
	//   - 审计记录的 ID 由存储层生成（例如数据库自增、全局 ID 生成器等）；
	//   - 调用方可以传入 record.ID=0 表示“未设置”，存储实现应负责生成。
	SaveAuditRecord(ctx context.Context, record AuditRecord) (int64, error)
}

// IAuditQueryStore 是审计轨迹的读取端口（D11）。
//
// 读取按资源配置将规约作用于实体标识和维度快照；entityID 为空返回 errors.InvalidInput，
// 无记录返回空 slice + nil。
type IAuditQueryStore interface {
	ListAuditRecords(ctx context.Context, entityID string, offset, limit int, s spec.Spec) ([]AuditRecord, error)
}

// IBulkAuditStore 定义审计记录的批量写入能力（可选扩展）。
//
// 说明：
// - 用于批量写场景减少 N 次 SaveAuditRecord 的往返开销；
// - 不强制所有 IAuditStore 实现支持批量写入，audited application 会自动降级为逐条写入。
type IBulkAuditStore interface {
	SaveAuditRecords(ctx context.Context, records []AuditRecord) error
}

// IRestoreRepository 表示支持“读取已软删实体并恢复”的仓储能力。
//
// 说明：
// - `Get(ctx, id, s)` 只返回未删除实体（deleted_at IS NULL）；
// - Restore 需要拿到“包含已删除”的实体以驱动状态机（Restore）并更新落库；
// - 两个方法与普通端口一样受规约约束，已软删记录只能经本组端口读写。
type IRestoreRepository[T domain.IEntity[ID], ID comparable] interface {
	// GetWithDeleted 读取实体（包含已删除记录）。
	GetWithDeleted(ctx context.Context, id ID, s spec.Spec) (T, error)

	// Restore 恢复实体；要求当前行已软删，作为原子条件。
	Restore(ctx context.Context, e T, s spec.Spec) error
}

// IDeletedQueryRepository 表示支持“查询已删除列表”的仓储能力。
//
// 说明：
// - 该能力用于 audited 的 DeletedList 等用例；
// - 默认 ORM Repo 会对实现 `domain.ISoftDeletable` 的实体提供该能力。
type IDeletedQueryRepository[T domain.IEntity[ID], ID comparable] interface {
	// ListDeleted 返回已软删的实体列表（仅删除态），按规约过滤。
	ListDeleted(ctx context.Context, offset, limit int, s spec.Spec) ([]T, error)
}
