package domain

import "time"

// ITimestamps 抽象Timestamps能力接口。
type ITimestamps interface {
	GetCreatedAt() time.Time
	SetCreatedAt(t time.Time)
	GetUpdatedAt() time.Time
	SetUpdatedAt(t time.Time)
}

// Timestamps 默认生命周期时间戳实现（用于嵌入）。
//
// 注意：
// - 该结构体不包含 DeletedAt/DeletedBy；
// - 软删除语义由 `domain.ISoftDeletable` 承担。
type Timestamps struct {
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

var _ ITimestamps = (*Timestamps)(nil)

// GetCreatedAt 返回创建时间。
func (t *Timestamps) GetCreatedAt() time.Time { return t.CreatedAt }

// SetCreatedAt 设置创建时间。
func (t *Timestamps) SetCreatedAt(v time.Time) { t.CreatedAt = v }

// GetUpdatedAt 返回最近一次更新时间。
func (t *Timestamps) GetUpdatedAt() time.Time { return t.UpdatedAt }

// SetUpdatedAt 设置最近一次更新时间。
func (t *Timestamps) SetUpdatedAt(v time.Time) { t.UpdatedAt = v }
