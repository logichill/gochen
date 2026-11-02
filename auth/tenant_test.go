package auth

import (
	"gochen/domain/meta"
	"testing"
)

func TestTenantEntityDimension(t *testing.T) {
	entity := &TenantEntity[int64]{}
	entity.SetTenantID("tenant-a")
	metadata, err := meta.Describe[*TenantEntity[int64]]()
	if err != nil {
		t.Fatal(err)
	}
	value, err := metadata.Read(entity, meta.ByDimension("tenant"))
	if err != nil || value != entity.GetTenantID() {
		t.Fatalf("tenant binding: %v, %v", value, err)
	}
}
