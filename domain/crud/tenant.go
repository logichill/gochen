package crud

import (
	"context"
	"gochen/domain"
)

// ITenantEntity 租户感知实体接口。
//
// 说明：实体需要实现此接口以支持多租户隔离。
type ITenantEntity[ID comparable] interface {
	domain.IEntity[ID]
	GetTenantID() string
	SetTenantID(tenantID string)
}

// TenantEntity 租户感知实体基础类型。
//
// 说明：
// - 扩展自 Entity，添加租户隔离字段；
// - 所有多租户实体应嵌入此类型或实现 ITenantEntity 接口。
//
// 用法示例：
//
//	type User struct {
//	    crud.TenantEntity[int64]
//	    domain.Timestamps
//	    Name  string `json:"name"`
//	    Email string `json:"email"`
//	}
type TenantEntity[ID comparable] struct {
	Entity[ID]
	TenantID string `json:"tenant_id"`
}

var (
	_ domain.IEntity[int64] = (*TenantEntity[int64])(nil)
	_ ITenantEntity[int64]  = (*TenantEntity[int64])(nil)
)

func (e *TenantEntity[ID]) GetTenantID() string {
	return e.TenantID
}

// SetTenantID 设置实体的租户标识。
func (e *TenantEntity[ID]) SetTenantID(tenantID string) {
	e.TenantID = tenantID
}

// ITenantRepository 租户感知仓储接口。
//
// 说明：
// - 扩展 IRepository，表达“实体按 tenant 归属隔离”的领域约束；
// - 当前 tenant 的解析与注入属于应用层职责，不在 domain/crud 中实现。
type ITenantRepository[T ITenantEntity[ID], ID comparable] interface {
	IRepository[T, ID]
	IQueryRepository[T, ID]

	// ListByTenant 按租户 ID 分页查询（用于管理后台跨租户场景）
	ListByTenant(ctx context.Context, tenantID string, offset, limit int) ([]T, error)

	// CountByTenant 按租户 ID 统计总数
	CountByTenant(ctx context.Context, tenantID string) (int64, error)
}
