package auth

import (
	"gochen/domain"
	"gochen/domain/crud"
)

// ITenantEntity 是授权层租户便利实体的访问契约。
type ITenantEntity[ID comparable] interface {
	domain.IEntity[ID]
	GetTenantID() string
	SetTenantID(string)
}

// TenantEntity 提供采用推荐 tenant 维度的便利字段声明。
type TenantEntity[ID comparable] struct {
	crud.Entity[ID]
	TenantID string `json:"tenant_id" dim:"tenant"`
}

// GetTenantID 返回租户标识。
func (e *TenantEntity[ID]) GetTenantID() string { return e.TenantID }

// SetTenantID 设置租户标识。
func (e *TenantEntity[ID]) SetTenantID(id string) { e.TenantID = id }
