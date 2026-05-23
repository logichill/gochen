package crud

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"gochen/domain"
)

// testTenantUser 测试用租户实体
type testTenantUser struct {
	TenantEntity[int64]
	Name string
}

func TestTenantEntity_GetSetTenantID(t *testing.T) {
	user := &testTenantUser{}
	user.ID = 1
	user.Name = "test"

	assert.Equal(t, "", user.GetTenantID())

	user.SetTenantID("tenant-1")
	assert.Equal(t, "tenant-1", user.GetTenantID())
}

func TestTenantEntity_ImplementsInterfaces(t *testing.T) {
	var e interface{} = &TenantEntity[int64]{}

	_, ok := e.(domain.IEntity[int64])
	assert.True(t, ok, "TenantEntity should implement domain.IEntity")

	_, ok = e.(ITenantEntity[int64])
	assert.True(t, ok, "TenantEntity should implement ITenantEntity")
}
