package repo

import (
	"strings"

	"gochen/domain"
	"gochen/domain/access"
	"gochen/ident"
)

// Option 用于配置默认 ORM 仓储行为。
type Option[T domain.IEntity[ID], ID comparable] func(*Repo[T, ID])

func WithIDGenerator[T domain.IEntity[ID], ID comparable](gen ident.IGenerator[ID]) Option[T, ID] {
	return func(r *Repo[T, ID]) {
		if gen == nil {
			return
		}
		r.idGen = gen
	}
}

// WithDefaultActor 配置默认操作人（当 ctx 中未携带 operator 时使用）。
func WithDefaultActor[T domain.IEntity[ID], ID comparable](actor string) Option[T, ID] {
	return func(r *Repo[T, ID]) {
		if actor == "" {
			return
		}
		r.defaultActor = actor
	}
}

// WithSoftDeleteColumns 配置软删除列名（可选）。
func WithSoftDeleteColumns[T domain.IEntity[ID], ID comparable](deletedAtCol, deletedByCol string) Option[T, ID] {
	deletedAtCol = strings.TrimSpace(deletedAtCol)
	deletedByCol = strings.TrimSpace(deletedByCol)
	return func(r *Repo[T, ID]) {
		if r == nil || deletedAtCol == "" {
			return
		}
		r.softDelete = true
		r.softDeleteCols = softDeleteColumns{DeletedAt: deletedAtCol, DeletedBy: deletedByCol}
	}
}

// WithResourceKind 配置仓储对应的授权资源种类。
func WithResourceKind[T domain.IEntity[ID], ID comparable](kind string) Option[T, ID] {
	kind = strings.TrimSpace(kind)
	return func(r *Repo[T, ID]) {
		if r == nil || kind == "" {
			return
		}
		r.resourceKind = kind
	}
}

// WithDataScopeResolver 配置 repo 使用的数据边界解析器。
func WithDataScopeResolver[T domain.IEntity[ID], ID comparable](resolver access.IDataScopeResolver) Option[T, ID] {
	return func(r *Repo[T, ID]) {
		if r == nil || resolver == nil {
			return
		}
		r.dataScope = resolver
	}
}

// WithAccessColumns 显式配置 managed scope / owner / version 依赖的列名。
func WithAccessColumns[T domain.IEntity[ID], ID comparable](managedScopeCol, ownerIDCol, versionCol string) Option[T, ID] {
	columns := accessColumns{
		managedScope: strings.TrimSpace(managedScopeCol),
		ownerID:      strings.TrimSpace(ownerIDCol),
		version:      strings.TrimSpace(versionCol),
	}
	return func(r *Repo[T, ID]) {
		if r == nil {
			return
		}
		if columns.managedScope != "" {
			r.accessColumns.managedScope = columns.managedScope
		}
		if columns.ownerID != "" {
			r.accessColumns.ownerID = columns.ownerID
		}
		if columns.version != "" {
			r.accessColumns.version = columns.version
		}
	}
}
