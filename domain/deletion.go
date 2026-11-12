package domain

import (
	"gochen/errors"
	"strings"
	"time"
)

// ISoftDeletableBy 表达可选的删除操作者归因。
type ISoftDeletableBy interface{ SoftDeleteBy(string, time.Time) error }

// DeletionState 保存独立于审计记录和修改归因的删除状态。
type DeletionState struct {
	DeletedAt *time.Time `json:"deleted_at,omitempty" managed:"true"`
	DeletedBy *string    `json:"deleted_by,omitempty" managed:"true"`
}

// GetDeletedAt 返回删除时间；未删除时返回 nil。
func (e *DeletionState) GetDeletedAt() *time.Time { return e.DeletedAt }

// GetDeletedBy 返回删除操作人；未记录时返回 nil。
func (e *DeletionState) GetDeletedBy() *string { return e.DeletedBy }

// SetDeletedInfo 恢复由软删除流程管理的删除信息。
func (e *DeletionState) SetDeletedInfo(at *time.Time, by *string) {
	e.DeletedAt = cloneTimePointer(at)
	e.DeletedBy = cloneStringPointer(by)
}

// IsDeleted 判断当前是否处于删除态。
func (e *DeletionState) IsDeleted() bool { return e.DeletedAt != nil }

// SoftDelete 标记删除态，不记录操作人。
func (e *DeletionState) SoftDelete(at time.Time) error {
	if e.IsDeleted() {
		return errors.NewCode(errors.Conflict, "entity already deleted")
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
func (e *DeletionState) SoftDeleteBy(by string, at time.Time) error {
	by = strings.TrimSpace(by)
	if by == "" {
		return errors.NewCode(errors.InvalidInput, "operator is required")
	}
	if e.IsDeleted() {
		return errors.NewCode(errors.Conflict, "entity already deleted")
	}
	e.DeletedAt = &at
	e.DeletedBy = &by
	return nil
}

// Restore 恢复被软删除的实体。
func (e *DeletionState) Restore() error {
	if !e.IsDeleted() {
		return errors.NewCode(errors.Conflict, "entity not deleted")
	}
	e.DeletedAt = nil
	e.DeletedBy = nil
	return nil
}

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
