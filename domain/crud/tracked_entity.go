package crud

import (
	"gochen/errors"
	"strings"
	"time"
)

// TrackedEntity 组合常用托管字段，审计与删除能力均不要求使用该类型。
type TrackedEntity[ID comparable] struct {
	Entity[ID]
	CreatedAt time.Time  `json:"created_at" managed:"true"`
	CreatedBy string     `json:"created_by" managed:"true"`
	UpdatedAt time.Time  `json:"updated_at" managed:"true"`
	UpdatedBy string     `json:"updated_by" managed:"true"`
	DeletedAt *time.Time `json:"deleted_at,omitempty" managed:"true"`
	DeletedBy *string    `json:"deleted_by,omitempty" managed:"true"`
}

// GetCreatedAt 返回实体的创建时间。
func (e *TrackedEntity[ID]) GetCreatedAt() time.Time { return e.CreatedAt }

// GetCreatedBy 返回创建操作人。
func (e *TrackedEntity[ID]) GetCreatedBy() string { return e.CreatedBy }

// GetUpdatedAt 返回最近一次更新的时间。
func (e *TrackedEntity[ID]) GetUpdatedAt() time.Time { return e.UpdatedAt }

// GetUpdatedBy 返回最近修改操作人。
func (e *TrackedEntity[ID]) GetUpdatedBy() string { return e.UpdatedBy }

// SetCreatedInfo 一次性写入创建人和创建时间。
func (e *TrackedEntity[ID]) SetCreatedInfo(by string, at time.Time) {
	e.CreatedBy = by
	e.CreatedAt = at
}

// SetCreatedAt 设置创建时间。
func (e *TrackedEntity[ID]) SetCreatedAt(t time.Time) {
	e.CreatedAt = t
}

// SetUpdatedInfo 一次性写入更新人和更新时间；版本由仓储根据写入前版本推进。
func (e *TrackedEntity[ID]) SetUpdatedInfo(by string, at time.Time) {
	e.UpdatedBy = by
	e.UpdatedAt = at
}

// SetUpdatedAt 设置最近修改时间。
func (e *TrackedEntity[ID]) SetUpdatedAt(t time.Time) {
	e.UpdatedAt = t
}

// SetUpdatedBy 设置最近更新人，不推进乐观锁版本。
func (e *TrackedEntity[ID]) SetUpdatedBy(by string) {
	e.UpdatedBy = by
}

// GetDeletedAt 返回删除时间；未删除时返回 nil。
func (e *TrackedEntity[ID]) GetDeletedAt() *time.Time { return e.DeletedAt }

// GetDeletedBy 返回删除操作人；未记录时返回 nil。
func (e *TrackedEntity[ID]) GetDeletedBy() *string { return e.DeletedBy }

// SetDeletedInfo 恢复由软删除流程管理的删除信息。
func (e *TrackedEntity[ID]) SetDeletedInfo(at *time.Time, by *string) {
	e.DeletedAt = cloneTimePointer(at)
	e.DeletedBy = cloneStringPointer(by)
}

// IsDeleted 判断当前是否处于删除态。
func (e *TrackedEntity[ID]) IsDeleted() bool { return e.DeletedAt != nil }

// SoftDelete 标记删除态，不记录操作人。
func (e *TrackedEntity[ID]) SoftDelete(at time.Time) error {
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
func (e *TrackedEntity[ID]) SoftDeleteBy(by string, at time.Time) error {
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
func (e *TrackedEntity[ID]) Restore() error {
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
func (e *TrackedEntity[ID]) Validate() error { return nil }

func cloneTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func cloneStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}
