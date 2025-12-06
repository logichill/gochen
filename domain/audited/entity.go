// Package audited 提供审计/软删除场景下的实体接口与通用实现。
// 用于配合 `gochen/domain/audited` 的仓储与服务抽象。
package audited

import (
	"time"

	"gochen/domain"
)

// IAuditable 审计追踪接口。
type IAuditable interface {
	// 创建信息
	GetCreatedAt() time.Time
	GetCreatedBy() string

	// 最后修改信息
	GetUpdatedAt() time.Time
	GetUpdatedBy() string

	// 设置审计信息（由基础设施层调用）
	SetCreatedInfo(by string, at time.Time)
	SetUpdatedInfo(by string, at time.Time)
}

// ISoftDeletable 软删除接口。
type ISoftDeletable interface {
	GetDeletedAt() *time.Time
	GetDeletedBy() *string
	IsDeleted() bool
	SoftDelete(by string, at time.Time) error
	Restore() error
}

// IAuditedEntity 带审计与软删除能力的实体接口。
type IAuditedEntity[T comparable] interface {
	domain.IEntity[T]
	IAuditable
	ISoftDeletable
	domain.IValidatable
}

// Entity 通用审计实体字段（用于嵌入），默认使用 int64 作为主键类型。
type Entity struct {
	ID        int64      `json:"id"`
	Version   uint64     `json:"version"`
	CreatedAt time.Time  `json:"created_at"`
	CreatedBy string     `json:"created_by"`
	UpdatedAt time.Time  `json:"updated_at"`
	UpdatedBy string     `json:"updated_by"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
	DeletedBy *string    `json:"deleted_by,omitempty"`
}

func (e *Entity) GetID() int64       { return e.ID }
func (e *Entity) GetVersion() uint64 { return e.Version }

func (e *Entity) GetCreatedAt() time.Time { return e.CreatedAt }
func (e *Entity) GetCreatedBy() string    { return e.CreatedBy }
func (e *Entity) GetUpdatedAt() time.Time { return e.UpdatedAt }
func (e *Entity) GetUpdatedBy() string    { return e.UpdatedBy }

func (e *Entity) SetCreatedInfo(by string, at time.Time) {
	e.CreatedBy = by
	e.CreatedAt = at
}

func (e *Entity) SetUpdatedInfo(by string, at time.Time) {
	e.UpdatedBy = by
	e.UpdatedAt = at
	// 所有“修改类”操作都会推动版本号递增，便于审计与乐观锁控制；
	// 软删/恢复等方法也会递增版本，保持语义一致。
	e.Version++
}

func (e *Entity) GetDeletedAt() *time.Time { return e.DeletedAt }
func (e *Entity) GetDeletedBy() *string    { return e.DeletedBy }
func (e *Entity) IsDeleted() bool          { return e.DeletedAt != nil }

func (e *Entity) SoftDelete(by string, at time.Time) error {
	if e.IsDeleted() {
		return domain.NewAlreadyDeletedError(e.ID)
	}
	e.DeletedAt = &at
	e.DeletedBy = &by
	e.Version++
	return nil
}

func (e *Entity) Restore() error {
	if !e.IsDeleted() {
		return domain.NewNotDeletedError(e.ID)
	}
	e.DeletedAt = nil
	e.DeletedBy = nil
	e.Version++
	return nil
}

// 审计/软删相关错误哨兵（用于 errors.Is 比较）
// 已迁移到 domain 包，这里提供别名以保持向后兼容
var (
	// Deprecated: 使用 domain.ErrAlreadyDeleted() 代替
	ErrAlreadyDeleted = domain.ErrAlreadyDeleted()
	// Deprecated: 使用 domain.ErrNotDeleted() 代替
	ErrNotDeleted = domain.ErrNotDeleted()
	// Deprecated: 使用 domain.ErrInvalidVersion() 代替
	ErrInvalidVersion = domain.ErrInvalidVersion()
)
