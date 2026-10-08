// Package capability 按实际业务能力组合安全外壳；批量与审计入口必须使用已授权的实现。
package capability

import (
	appaudited "gochen/app/audited"
	"gochen/app/crud"
	"gochen/domain"
)

// Parts 描述组合外壳所需的各能力实现。
type Parts[T domain.IEntity[ID], ID comparable] struct {
	// Core 是已加固的核心 Application，必填。
	Core crud.IApplication[T, ID]
	// Batch 是已加固的批量写实现；内层不支持批量时留 nil。
	Batch crud.IBatchWriter[T, ID]
	// Audited 是已加固的审计扩展实现；内层非审计 Application 时留 nil。
	Audited appaudited.IAuditOperations[T, ID]
}

// Detect 按内层实际能力填充探测结果，供调用方决定要加固哪些能力面。
type Detected[T domain.IEntity[ID], ID comparable] struct {
	Batch   crud.IBatchWriter[T, ID]
	Audited appaudited.IAuditOperations[T, ID]
}

// Detect 探测内层 Application 提供了哪些可选能力。
func Detect[T domain.IEntity[ID], ID comparable](inner crud.IApplication[T, ID]) Detected[T, ID] {
	var out Detected[T, ID]
	if batch, ok := any(inner).(crud.IBatchWriter[T, ID]); ok {
		out.Batch = batch
	}
	if surface, ok := any(inner).(appaudited.IAuditOperations[T, ID]); ok {
		out.Audited = surface
	}
	return out
}

// --- 组合外壳 ---
//
// 批量与审计两个能力轴共 4 种外壳。嵌入接口而非手工转发，
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
	appaudited.IAuditOperations[T, ID]
}

type shellBD[T domain.IEntity[ID], ID comparable] struct {
	crud.IApplication[T, ID]
	crud.IBatchWriter[T, ID]
	appaudited.IAuditOperations[T, ID]
}

// Compose 按实际具备的能力返回对应外壳。
//
// 返回值始终是 appcrud.IApplication；额外能力靠动态类型暴露给探测方。
func Compose[T domain.IEntity[ID], ID comparable](parts Parts[T, ID]) crud.IApplication[T, ID] {
	hasBatch := parts.Batch != nil
	hasAudited := parts.Audited != nil

	switch {
	case hasBatch && hasAudited:
		return &shellBD[T, ID]{parts.Core, parts.Batch, parts.Audited}
	case hasBatch:
		return &shellB[T, ID]{parts.Core, parts.Batch}
	case hasAudited:
		return &shellD[T, ID]{parts.Core, parts.Audited}
	default:
		return &shell[T, ID]{parts.Core}
	}
}
