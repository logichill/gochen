// Package capability 负责在安全装饰器包装 Application 时**保住可选能力接口**。
//
// 为什么需要它：
//
// 常见误区是认为"Application 层面对的 IApplication 是宽接口，所以装饰器没有
// 丢接口的问题"。这个前提不成立——api/rest 恰恰在 Application 上做可选能力探测：
//
//	appcrud.IBatchWriter          → api/rest/capabilities.go   批量路由
//	IAuditedService（结构等价）    → api/rest/router.go         purge/restore/审计轨迹路由
//	IServiceConfigUpdatable       → api/rest/builder.go        装配期配置注入
//	IValidatorAware               → api/rest/builder.go        装配期校验器注入
//	IHooksAware                   → api/rest/builder.go        装配期钩子注入
//
// 只实现 IApplication 本体的装饰器会让这些断言全部失败：批量与审计路由静默消失，
// 装配期注入直接报 FailedPrecondition。
//
// 本包的解法是**按实际能力组合出装饰器外壳**，两条铁律：
//
//  1. 有安全语义的能力（批量写、审计高危操作）必须由调用方传入**已加固的实现**，
//     绝不直接转发到裸 Application——否则批量入口就成了绕过授权的后门；
//  2. 没有安全语义的装配期注入（config / validator / hooks）原样委托给内层。
//
// 能力缺失时对应外壳就不实现该接口，探测如实失败——不假冒能力，
// 保住 api/rest 构造期 fail-fast 的有效性。
package capability

import (
	"context"

	"gochen/app/crud"
	"gochen/domain"
	"gochen/domain/audited"
)

// IAuditedSurface 是审计扩展中**除 Delete 之外**的能力面。
//
// Delete 由核心 IApplication 提供（且已被安全装饰），此处排除以免嵌入冲突；
// 方法集与 api/rest 的 IAuditedService 结构等价，因此组合后的外壳
// 仍能通过 REST 侧的能力探测。
type IAuditedSurface[T domain.IEntity[ID], ID comparable] interface {
	Purge(ctx context.Context, id ID) error
	Restore(ctx context.Context, id ID, by string) error
	ListDeleted(ctx context.Context, offset, limit int) ([]T, error)
	AuditTrail(ctx context.Context, id ID, offset, limit int) ([]audited.AuditRecord, error)
	AuditStore() audited.IAuditStore
}

// IAssemblyAware 聚合装配期注入能力。
//
// 三者在 crud.Application 上恒同时存在，故按整体探测；它们不含安全语义，
// 直接委托内层即可。
type IAssemblyAware[T domain.IEntity[ID], ID comparable] interface {
	crud.IServiceConfigUpdatable
	crud.IValidatorAware
	crud.IHooksAware[T, ID]
}

// Parts 描述组合外壳所需的各能力实现。
type Parts[T domain.IEntity[ID], ID comparable] struct {
	// Core 是已加固的核心 Application，必填。
	Core crud.IApplication[T, ID]
	// Batch 是已加固的批量写实现；内层不支持批量时留 nil。
	Batch crud.IBatchWriter[T, ID]
	// Audited 是已加固的审计扩展实现；内层非审计 Application 时留 nil。
	Audited IAuditedSurface[T, ID]
	// Aware 是装配期注入的委托目标（内层自身）；内层不支持时留 nil。
	Aware IAssemblyAware[T, ID]
}

// Detect 按内层实际能力填充探测结果，供调用方决定要加固哪些能力面。
type Detected[T domain.IEntity[ID], ID comparable] struct {
	Batch   crud.IBatchWriter[T, ID]
	Audited IAuditedSurface[T, ID]
	Aware   IAssemblyAware[T, ID]
}

// Detect 探测内层 Application 提供了哪些可选能力。
func Detect[T domain.IEntity[ID], ID comparable](inner crud.IApplication[T, ID]) Detected[T, ID] {
	var out Detected[T, ID]
	if batch, ok := any(inner).(crud.IBatchWriter[T, ID]); ok {
		out.Batch = batch
	}
	if surface, ok := any(inner).(IAuditedSurface[T, ID]); ok {
		out.Audited = surface
	}
	if aware, ok := any(inner).(IAssemblyAware[T, ID]); ok {
		out.Aware = aware
	}
	return out
}

// --- 组合外壳 ---
//
// 三个能力轴各自可有可无，故有 8 种外壳。嵌入接口而非手工转发，
// 保证新增 IApplication 方法时无需逐个改这些外壳。

type shell[T domain.IEntity[ID], ID comparable] struct {
	crud.IApplication[T, ID]
}

type shellB[T domain.IEntity[ID], ID comparable] struct {
	crud.IApplication[T, ID]
	crud.IBatchWriter[T, ID]
}

type shellD[T domain.IEntity[ID], ID comparable] struct {
	crud.IApplication[T, ID]
	IAuditedSurface[T, ID]
}

type shellW[T domain.IEntity[ID], ID comparable] struct {
	crud.IApplication[T, ID]
	IAssemblyAware[T, ID]
}

type shellBD[T domain.IEntity[ID], ID comparable] struct {
	crud.IApplication[T, ID]
	crud.IBatchWriter[T, ID]
	IAuditedSurface[T, ID]
}

type shellBW[T domain.IEntity[ID], ID comparable] struct {
	crud.IApplication[T, ID]
	crud.IBatchWriter[T, ID]
	IAssemblyAware[T, ID]
}

type shellDW[T domain.IEntity[ID], ID comparable] struct {
	crud.IApplication[T, ID]
	IAuditedSurface[T, ID]
	IAssemblyAware[T, ID]
}

type shellBDW[T domain.IEntity[ID], ID comparable] struct {
	crud.IApplication[T, ID]
	crud.IBatchWriter[T, ID]
	IAuditedSurface[T, ID]
	IAssemblyAware[T, ID]
}

// Compose 按实际具备的能力返回对应外壳。
//
// 返回值始终是 appcrud.IApplication；额外能力靠动态类型暴露给探测方。
func Compose[T domain.IEntity[ID], ID comparable](parts Parts[T, ID]) crud.IApplication[T, ID] {
	hasBatch := parts.Batch != nil
	hasAudited := parts.Audited != nil
	hasAware := parts.Aware != nil

	switch {
	case hasBatch && hasAudited && hasAware:
		return &shellBDW[T, ID]{parts.Core, parts.Batch, parts.Audited, parts.Aware}
	case hasBatch && hasAudited:
		return &shellBD[T, ID]{parts.Core, parts.Batch, parts.Audited}
	case hasBatch && hasAware:
		return &shellBW[T, ID]{parts.Core, parts.Batch, parts.Aware}
	case hasAudited && hasAware:
		return &shellDW[T, ID]{parts.Core, parts.Audited, parts.Aware}
	case hasBatch:
		return &shellB[T, ID]{parts.Core, parts.Batch}
	case hasAudited:
		return &shellD[T, ID]{parts.Core, parts.Audited}
	case hasAware:
		return &shellW[T, ID]{parts.Core, parts.Aware}
	default:
		return &shell[T, ID]{parts.Core}
	}
}
