package crud

import (
	"testing"

	"gochen/domain"
)

// TestEntity_ImplementsInterfaces 验证 Entity ImplementsInterfaces。
func TestEntity_ImplementsInterfaces(t *testing.T) {
	e := &Entity[int64]{ID: 123, Version: 5}

	var _ domain.IEntity[int64] = e
	var _ domain.ISettableID[int64] = e

	if got := e.GetID(); got != 123 {
		t.Fatalf("GetID() = %d, want 123", got)
	}
	if got := e.GetVersion(); got != 5 {
		t.Fatalf("GetVersion() = %d, want 5", got)
	}

	e.SetID(456)
	if got := e.GetID(); got != 456 {
		t.Fatalf("after SetID(456), GetID() = %d, want 456", got)
	}
}

// TestEntity_ZeroValue 验证 Entity ZeroValue。
func TestEntity_ZeroValue(t *testing.T) {
	var e Entity[int64]

	if got := e.GetID(); got != 0 {
		t.Fatalf("zero value GetID() = %d, want 0", got)
	}
	if got := e.GetVersion(); got != 0 {
		t.Fatalf("zero value GetVersion() = %d, want 0", got)
	}
}

// TestEntity_Embedding 验证 Entity Embedding。
func TestEntity_Embedding(t *testing.T) {
	type User struct {
		Entity[int64]
		Name string `json:"name"`
	}

	u := &User{
		Entity: Entity[int64]{ID: 789, Version: 1},
		Name:   "alice",
	}

	var _ domain.IEntity[int64] = u
	var _ domain.ISettableID[int64] = u

	if got := u.GetID(); got != 789 {
		t.Fatalf("embedded GetID() = %d, want 789", got)
	}
	if got := u.GetVersion(); got != 1 {
		t.Fatalf("embedded GetVersion() = %d, want 1", got)
	}

	u.SetID(1000)
	if got := u.GetID(); got != 1000 {
		t.Fatalf("after SetID(1000), GetID() = %d, want 1000", got)
	}
}

// TestEntity_StringID 验证 Entity StringID。
func TestEntity_StringID(t *testing.T) {
	// 验证泛型 ID 类型支持字符串
	e := &Entity[string]{ID: "uuid-123", Version: 1}

	var _ domain.IEntity[string] = e

	if got := e.GetID(); got != "uuid-123" {
		t.Fatalf("GetID() = %s, want uuid-123", got)
	}

	e.SetID("uuid-456")
	if got := e.GetID(); got != "uuid-456" {
		t.Fatalf("after SetID, GetID() = %s, want uuid-456", got)
	}
}
