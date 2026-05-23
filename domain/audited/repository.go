// Package audited 提供带审计能力的仓储抽象。
// 在 CRUD 仓储能力基础上扩展审计轨迹与软删/恢复能力，
// 面向“有审计/软删需求”的领域模型提供更语义化的导入路径：`domain/audited`。
package audited

import (
	"context"
	"encoding/json"

	"gochen/domain"
	"gochen/domain/crud"
	"time"
)

// AuditOperation 审计操作类型（字符串枚举）
type AuditOperation string

// 预定义审计操作常量。
const (
	// AuditOpCreate 创建操作
	AuditOpCreate AuditOperation = "CREATE"
	// AuditOpUpdate 更新操作
	AuditOpUpdate AuditOperation = "UPDATE"
	// AuditOpDelete 软删除操作
	AuditOpDelete AuditOperation = "DELETE"
	// AuditOpDeleteHard 硬删除（永久删除）操作
	AuditOpDeleteHard AuditOperation = "DELETE_HARD"
	// AuditOpRestore 恢复（取消软删除）操作
	AuditOpRestore AuditOperation = "RESTORE"
)

// AuditRecord 审计记录。
type AuditRecord struct {
	ID        int64           `json:"id"`
	EntityID  string          `json:"entity_id"`
	Operation AuditOperation  `json:"operation"` // CREATE, UPDATE, DELETE, DELETE_HARD, RESTORE
	Operator  string          `json:"operator"`
	Timestamp time.Time       `json:"timestamp"`
	Changes   json.RawMessage `json:"changes,omitempty"` // 变更详情（JSON）
	Metadata  json.RawMessage `json:"metadata,omitempty"`
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
//   - 审计记录的写入应在仓储实现或服务层通过 IAuditStore 之类的装饰器完成，
//     避免在抽象接口上堆砌过多特定方法；
//   - 软删/恢复等操作可以由服务层基于 IEntity + domain.ISoftDeletable 组合实现。
type IAuditedRepository[T IAuditedEntity[ID], ID comparable] interface {
	crud.IRepository[T, ID]
	crud.IQueryRepository[T, ID]
}

// IAuditStore 审计记录存储接口。
//
// 典型实现可以是独立的审计表/集合（如 article_audits），
// 由审计型仓储或服务在 Create/Update/Delete/SoftDelete/Restore 等操作后写入，
// 查询时则按实体维度拉取审计轨迹。
//
// # 错误契约。
//
//   - SaveAuditRecord: 参数校验失败（entityID/operator/operation 为空等）返回 errors.InvalidInput；
//     其他为基础设施错误。审计写入失败是否应导致业务写失败取决于应用层策略（当前 audited app 倾向 fail-fast）
//   - ListAuditRecordsByEntity: entityID 为空返回 errors.InvalidInput；无记录返回空 slice + nil
type IAuditStore interface {
	// SaveAuditRecord 保存一条审计记录，并返回生成的审计记录 ID。
	//
	// 约定：
	//   - 审计记录的 ID 由存储层生成（例如数据库自增、全局 ID 生成器等）；
	//   - 调用方可以传入 record.ID=0 表示“未设置”，存储实现应负责生成。
	SaveAuditRecord(ctx context.Context, record AuditRecord) (int64, error)

	// ListAuditRecordsByEntity 按实体 ID 查询审计记录（支持分页）。
	//
	// entityID 通常是实体主键的字符串形式，例如 fmt.Sprint(id)。
	ListAuditRecordsByEntity(ctx context.Context, entityID string, offset, limit int) ([]AuditRecord, error)
}

// IBatchAuditStore 定义审计记录的批量写入能力（可选扩展）。
//
// 说明：
// - 用于批量写场景减少 N 次 SaveAuditRecord 的往返开销；
// - 不强制所有 IAuditStore 实现支持批量写入，audited application 会自动降级为逐条写入。
type IBatchAuditStore interface {
	SaveAuditRecords(ctx context.Context, records []AuditRecord) error
}

// IRestoreRepository 表示支持“读取已软删实体并恢复”的仓储能力。
//
// 说明：
// - `Get(ctx, id)` 通常只返回未删除实体（deleted_at IS NULL），以符合“软删后对外不可见”的常见语义；
// - Restore 需要拿到“包含已删除”的实体以驱动状态机（Restore）并更新落库，因此抽象为可选扩展能力；
// - 默认 ORM Repo 会对实现 `domain.ISoftDeletable` 的实体提供该能力。
type IRestoreRepository[T domain.IEntity[ID], ID comparable] interface {
	// GetWithDeleted 读取实体（包含已删除记录）。
	GetWithDeleted(ctx context.Context, id ID) (T, error)
}

// IDeletedQueryRepository 表示支持“查询已删除列表”的仓储能力。
//
// 说明：
// - 该能力用于 audited 的 DeletedList 等用例；
// - 默认 ORM Repo 会对实现 `domain.ISoftDeletable` 的实体提供该能力。
type IDeletedQueryRepository[T domain.IEntity[ID], ID comparable] interface {
	// ListDeleted 返回已软删的实体列表（仅删除态）。
	ListDeleted(ctx context.Context, offset, limit int) ([]T, error)
}
