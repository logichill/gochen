package db

// NamingConvention 描述数据表全部切面列名的命名规范约定。
type NamingConvention struct {
	// L2 空间隔离
	TenantColumn string // 默认 "tenant_id"

	// L3 数据范围与归属
	ScopeColumn   string // 默认 "managed_scope_id"
	OwnerIDColumn string // 默认 "owner_id"

	// 乐观锁版本
	VersionColumn string // 默认 "version"

	// 审计轨迹
	CreatedAtColumn string // 默认 "created_at"
	CreatedByColumn string // 默认 "created_by"
	UpdatedAtColumn string // 默认 "updated_at"
	UpdatedByColumn string // 默认 "updated_by"

	// 软删除
	DeletedAtColumn string // 默认 "deleted_at"
	DeletedByColumn string // 默认 "deleted_by"
}

// DefaultNamingConvention 返回框架内置默认列名规范。
func DefaultNamingConvention() NamingConvention {
	return NamingConvention{
		TenantColumn:    "tenant_id",
		ScopeColumn:     "managed_scope_id",
		OwnerIDColumn:   "owner_id",
		VersionColumn:   "version",
		CreatedAtColumn: "created_at",
		CreatedByColumn: "created_by",
		UpdatedAtColumn: "updated_at",
		UpdatedByColumn: "updated_by",
		DeletedAtColumn: "deleted_at",
		DeletedByColumn: "deleted_by",
	}
}

// WithDefaults 返回用框架默认值补全所有未设置（空字符串）字段后的规范副本。
func (n NamingConvention) WithDefaults() NamingConvention {
	defaults := DefaultNamingConvention()
	if n.TenantColumn == "" {
		n.TenantColumn = defaults.TenantColumn
	}
	if n.ScopeColumn == "" {
		n.ScopeColumn = defaults.ScopeColumn
	}
	if n.OwnerIDColumn == "" {
		n.OwnerIDColumn = defaults.OwnerIDColumn
	}
	if n.VersionColumn == "" {
		n.VersionColumn = defaults.VersionColumn
	}
	if n.CreatedAtColumn == "" {
		n.CreatedAtColumn = defaults.CreatedAtColumn
	}
	if n.CreatedByColumn == "" {
		n.CreatedByColumn = defaults.CreatedByColumn
	}
	if n.UpdatedAtColumn == "" {
		n.UpdatedAtColumn = defaults.UpdatedAtColumn
	}
	if n.UpdatedByColumn == "" {
		n.UpdatedByColumn = defaults.UpdatedByColumn
	}
	if n.DeletedAtColumn == "" {
		n.DeletedAtColumn = defaults.DeletedAtColumn
	}
	if n.DeletedByColumn == "" {
		n.DeletedByColumn = defaults.DeletedByColumn
	}
	return n
}

// INamingConventionProvider 允许 ORM 或 Database 实例暴露全局命名规范。
type INamingConventionProvider interface {
	NamingConvention() NamingConvention
}
