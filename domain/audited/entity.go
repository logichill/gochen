// Package audited 提供审计/软删除场景下的实体接口与通用实现。
// 用于配合 `gochen/domain/audited` 的仓储与服务抽象。
package audited

import (
	"strings"
	"time"

	"gochen/domain"
	"gochen/domain/crud"
	"gochen/errors"
)

// IAuditable 审计追踪接口。
//
// 说明：
// - IAuditable 扩展了 ITimestamps，额外包含操作人（By）信息；
// - 实现 IAuditable 的实体同时满足 ITimestamps 接口。
type IAuditable interface {
	domain.ITimestamps

	// 创建信息
	GetCreatedBy() string

	// 最后修改信息
	GetUpdatedBy() string

	// 删除信息（可选）。
	//
	// 说明：
	// - 软删除不一定要求记录操作人；轻量场景可返回 nil；
	// - audited 场景下建议返回对应的 DeletedBy。
	GetDeletedBy() *string

	// 设置审计信息（由基础设施层调用）
	SetCreatedInfo(by string, at time.Time)
	SetUpdatedInfo(by string, at time.Time)
}

// IAuditedEntity 带审计与软删除能力的实体接口。
type IAuditedEntity[T comparable] interface {
	domain.IEntity[T]
	domain.IValidatable
	domain.ISoftDeletable
	ISoftDeletableBy
	IAuditable
}

// ISoftDeletableBy 表示“需要显式传入 operator/by”的软删除能力（审计语义）。
//
// 说明：
// - 该接口不替代 domain.ISoftDeletable，而是 audited 场景的补充能力；
// - by 为空（或仅包含空白）视为调用方错误，应返回 errors.InvalidInput。
type ISoftDeletableBy interface {
	SoftDeleteBy(by string, at time.Time) error
}

// AuditedEntity 通用审计实体字段（用于嵌入），支持泛型 ID。
//
// 类型参数：
//   - ID: 实体主键类型，必须是可比较类型（如 int64、string、uuid.UUID 等）
type AuditedEntity[ID comparable] struct {
	crud.Entity[ID]
	CreatedAt time.Time  `json:"created_at"`
	CreatedBy string     `json:"created_by"`
	UpdatedAt time.Time  `json:"updated_at"`
	UpdatedBy string     `json:"updated_by"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
	DeletedBy *string    `json:"deleted_by,omitempty"`
}

// GetCreatedAt 返回实体的创建时间。
func (e *AuditedEntity[ID]) GetCreatedAt() time.Time { return e.CreatedAt }

func (e *AuditedEntity[ID]) GetCreatedBy() string { return e.CreatedBy }

// GetUpdatedAt 返回最近一次更新的时间。
func (e *AuditedEntity[ID]) GetUpdatedAt() time.Time { return e.UpdatedAt }

func (e *AuditedEntity[ID]) GetUpdatedBy() string { return e.UpdatedBy }

// SetCreatedInfo 一次性写入创建人和创建时间。
func (e *AuditedEntity[ID]) SetCreatedInfo(by string, at time.Time) {
	e.CreatedBy = by
	e.CreatedAt = at
}

// SetCreatedAt 设置创建At。
func (e *AuditedEntity[ID]) SetCreatedAt(t time.Time) {
	e.CreatedAt = t
}

// SetUpdatedInfo 一次性写入更新人和更新时间，并推进乐观锁版本。
func (e *AuditedEntity[ID]) SetUpdatedInfo(by string, at time.Time) {
	e.UpdatedBy = by
	e.UpdatedAt = at
	// 所有"修改类"操作都会推动版本号递增，便于审计与乐观锁控制；
	// 约定：软删/恢复等操作在落库前也会调用 SetUpdatedInfo（由仓储/服务驱动），因此版本推进统一在此发生。
	e.Version++
}

// SetUpdatedAt 设置更新At。
func (e *AuditedEntity[ID]) SetUpdatedAt(t time.Time) {
	e.UpdatedAt = t
}

// GetDeletedAt 返回删除时间；未删除时返回 nil。
func (e *AuditedEntity[ID]) GetDeletedAt() *time.Time { return e.DeletedAt }

// GetDeletedBy 返回删除操作人；未记录时返回 nil。
func (e *AuditedEntity[ID]) GetDeletedBy() *string { return e.DeletedBy }

func (e *AuditedEntity[ID]) IsDeleted() bool { return e.DeletedAt != nil }

func (e *AuditedEntity[ID]) SoftDelete(at time.Time) error {
	if e.IsDeleted() {
		return errors.NewCode(errors.Conflict, "entity already deleted").WithContext("id", e.ID)
	}
	e.DeletedAt = &at
	e.DeletedBy = nil
	return nil
}

// SoftDeleteBy 将实体标记为软删除，并记录操作人及删除时间（审计语义）。
//
// 参数：
// - by：操作人标识（不能为空）。
// - at：删除时间。
func (e *AuditedEntity[ID]) SoftDeleteBy(by string, at time.Time) error {
	by = strings.TrimSpace(by)
	if by == "" {
		return errors.NewCode(errors.InvalidInput, "operator is required").WithContext("id", e.ID)
	}
	if e.IsDeleted() {
		return errors.NewCode(errors.Conflict, "entity already deleted").WithContext("id", e.ID)
	}
	e.DeletedAt = &at
	e.DeletedBy = &by
	return nil
}

// Restore 恢复被软删除的实体。
func (e *AuditedEntity[ID]) Restore() error {
	if !e.IsDeleted() {
		return errors.NewCode(errors.Conflict, "entity not deleted").WithContext("id", e.ID)
	}
	e.DeletedAt = nil
	e.DeletedBy = nil
	return nil
}

// Validate 默认不做额外校验。
//
// 说明：
// - 说明：业务实体通常会通过覆盖/额外方法补充领域约束校验；
// - 这里提供一个默认实现，避免在不需要额外校验的场景下重复写空实现。
func (e *AuditedEntity[ID]) Validate() error { return nil }

// 编译期接口检查。
var (
	_ domain.ITimestamps        = (*AuditedEntity[int64])(nil)
	_ IAuditable                = (*AuditedEntity[int64])(nil)
	_ domain.ISoftDeletable     = (*AuditedEntity[int64])(nil)
	_ domain.ISettableID[int64] = (*AuditedEntity[int64])(nil)
	_ ISoftDeletableBy          = (*AuditedEntity[int64])(nil)
	_ IAuditedEntity[int64]     = (*AuditedEntity[int64])(nil)
)
