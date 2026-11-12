package domain

import "time"

// IChangeInfo 表达创建和更新归因，不要求审计记录或软删除。
type IChangeInfo interface {
	ITimestamps
	GetCreatedBy() string
	GetUpdatedBy() string
	SetCreatedInfo(string, time.Time)
	SetUpdatedInfo(string, time.Time)
}

// ChangeInfo 保存创建和修改归因字段。
type ChangeInfo struct {
	CreatedAt time.Time `json:"created_at" managed:"true"`
	CreatedBy string    `json:"created_by" managed:"true"`
	UpdatedAt time.Time `json:"updated_at" managed:"true"`
	UpdatedBy string    `json:"updated_by" managed:"true"`
}

// GetCreatedAt 返回创建时间。
func (e *ChangeInfo) GetCreatedAt() time.Time { return e.CreatedAt }

// GetUpdatedAt 返回最近修改时间。
func (e *ChangeInfo) GetUpdatedAt() time.Time { return e.UpdatedAt }

// GetCreatedBy 返回创建操作人。
func (e *ChangeInfo) GetCreatedBy() string { return e.CreatedBy }

// GetUpdatedBy 返回最近修改操作人。
func (e *ChangeInfo) GetUpdatedBy() string { return e.UpdatedBy }

// SetCreatedAt 设置创建时间。
func (e *ChangeInfo) SetCreatedAt(v time.Time) { e.CreatedAt = v }

// SetUpdatedAt 设置最近修改时间。
func (e *ChangeInfo) SetUpdatedAt(v time.Time) { e.UpdatedAt = v }

// SetUpdatedBy 设置最近修改操作人，不改变版本。
func (e *ChangeInfo) SetUpdatedBy(v string) { e.UpdatedBy = v }

// SetCreatedInfo 同时设置创建操作人与时间。
func (e *ChangeInfo) SetCreatedInfo(by string, at time.Time) { e.CreatedBy = by; e.CreatedAt = at }

// SetUpdatedInfo 同时设置最近修改操作人与时间，不改变版本。
func (e *ChangeInfo) SetUpdatedInfo(by string, at time.Time) { e.UpdatedBy = by; e.UpdatedAt = at }
