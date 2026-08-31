package crud

import (
	"gochen/domain"
)

// ITenantEntity 租户感知实体接口。
//
// 两类消费方，性质不同，改动前务必分清：
//
//  1. **执行路径依赖**：`gochen/testkit` 的内存仓储没有"列"与反射映射，
//     GetTenantID / SetTenantID 就是它读写归属的唯一通道，不可替代；
//  2. **构造期意图断言**：`runtime/db/orm/repo` 的 SQL 仓储**并不调用**
//     本接口——它按 IsolationCols.Column 映射到的实体字段做反射盖戳。
//     那里断言本接口只是为了拦住"把 Column: tenant_id 配到一个根本没打算做
//     多租户的实体上"这类装配失误，属于意图校验而非能力校验；
//     真正保证"归属写得进去"的是仓储的 validateAccessColumns（隔离列必须可映射）。
//
// 因此：仓储通过 IsolationCols.Resolve 显式声明"隔离键不是租户"时，
// 实体**无需**实现本接口（归属由 Column 映射的字段承载）。
type ITenantEntity[ID comparable] interface {
	domain.IEntity[ID]
	GetTenantID() string
	SetTenantID(tenantID string)
}

// TenantEntity 租户感知实体基础类型。
//
// 定位：**多租户实体的标准字段声明**——统一字段名、`json:"tenant_id"` tag
// 与 getter/setter，使 tenant 归属在全生态形态一致。它不绑定任何一代隔离机制，
// 隔离能力如何落地（Repo 构造期显式列 / 内存仓储接口盖戳）与本类型无关。
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
