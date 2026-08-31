package crud

import "gochen/domain"

// Entity 通用 CRUD 实体基础类型，支持泛型 ID。
//
// 说明：
// - 仅包含必需的 ID 与乐观锁版本字段，适用于不需要审计/软删除/时间戳的简单场景；
// - 若需要生命周期时间戳（CreatedAt/UpdatedAt），请组合 `domain.Timestamps`；
// - 若需要审计/软删除能力，请使用 `domain/audited.AuditedEntity`（或业务自定义实现）。
//
// 用法示例：
//
//	type User struct {
//	    crud.Entity[int64]
//	    domain.Timestamps
//	    Name  string `json:"name"`
//	    Email string `json:"email"`
//	}
//
// 类型参数：
//   - ID: 实体主键类型，必须是可比较类型（如 int64、string、uuid.UUID 等）
type Entity[ID comparable] struct {
	ID      ID     `json:"id"`
	Version uint64 `json:"version"`
}

var (
	_ domain.IEntity[int64]     = (*Entity[int64])(nil)
	_ domain.ISettableID[int64] = (*Entity[int64])(nil)
)

func (e *Entity[ID]) GetID() ID { return e.ID }

func (e *Entity[ID]) GetVersion() uint64 { return e.Version }

// SetID 设置实体的唯一标识（用于默认仓储的 ID 回填）。
func (e *Entity[ID]) SetID(id ID) { e.ID = id }
