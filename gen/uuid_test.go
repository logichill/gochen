package gen

import (
	"testing"

	"gochen/gen/uuid"
)

func TestUUIDGenerators(t *testing.T) {
	v4Gen := NewUUIDGenerator()
	id4, err := v4Gen.Next()
	if err != nil {
		t.Fatalf("v4 Next() error: %v", err)
	}
	if !uuid.IsValid(id4) {
		t.Fatalf("v4 ID invalid: %s", id4)
	}

	v7Gen := NewUUIDv7Generator()
	id7, err := v7Gen.Next()
	if err != nil {
		t.Fatalf("v7 Next() error: %v", err)
	}
	if !uuid.IsValid(id7) {
		t.Fatalf("v7 ID invalid: %s", id7)
	}
}
