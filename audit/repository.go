package audit

import (
	"context"
	"encoding/json"
	"gochen/domain/spec"
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

// IAuditStore 审计记录存储接口。
//
// 典型实现可以是独立的审计表/集合（如 article_audits），
// 由启用审计的应用在 Create/Update/Delete/SoftDelete/Restore 等操作后写入，
// 查询时则按实体维度拉取审计轨迹。
//
// # 错误契约。
//
//   - SaveAuditRecord: 参数校验失败（entityID/action/operator 为空等）返回 errors.InvalidInput；
//     其他为基础设施错误。应用写入协议要求审计失败回滚整个业务事务
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
// - 不强制所有 IAuditStore 实现支持批量写入，应用执行协议会自动降级为逐条写入。
type IBulkAuditStore interface {
	SaveAuditRecords(ctx context.Context, records []AuditRecord) error
}
