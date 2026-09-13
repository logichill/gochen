package db_test

import (
	"testing"

	"gochen/db"
	"gochen/testkit/assert"
)

func TestNamingConventionDefaults(t *testing.T) {
	def := db.DefaultNamingConvention()
	assert.Equal(t, "tenant_id", def.TenantColumn)
	assert.Equal(t, "managed_scope_id", def.ScopeColumn)
	assert.Equal(t, "owner_id", def.OwnerIDColumn)
	assert.Equal(t, "version", def.VersionColumn)
	assert.Equal(t, "created_at", def.CreatedAtColumn)
	assert.Equal(t, "created_by", def.CreatedByColumn)
	assert.Equal(t, "updated_at", def.UpdatedAtColumn)
	assert.Equal(t, "updated_by", def.UpdatedByColumn)
	assert.Equal(t, "deleted_at", def.DeletedAtColumn)
	assert.Equal(t, "deleted_by", def.DeletedByColumn)
}

func TestNamingConvention_WithDefaults(t *testing.T) {
	partial := db.NamingConvention{
		TenantColumn: "custom_tenant",
	}
	filled := partial.WithDefaults()
	assert.Equal(t, "custom_tenant", filled.TenantColumn)
	assert.Equal(t, "managed_scope_id", filled.ScopeColumn)
	assert.Equal(t, "version", filled.VersionColumn)
	assert.Equal(t, "deleted_at", filled.DeletedAtColumn)
	assert.Equal(t, "deleted_by", filled.DeletedByColumn)
}
