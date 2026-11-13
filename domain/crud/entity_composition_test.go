package crud

import (
	"encoding/json"
	"gochen/domain"
	"gochen/domain/meta"
	"testing"
	"time"
)

type composedFields struct {
	Entity[int64]
	domain.ChangeInfo
	domain.DeletionState
}

func TestOptionalComponentsPreserveTrackedWireFields(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	by := "alice"
	identity := Entity[int64]{ID: 7, Version: 2}
	tracked := &TrackedEntity[int64]{Entity: identity, CreatedAt: at, CreatedBy: by, UpdatedAt: at, UpdatedBy: by, DeletedAt: &at, DeletedBy: &by}
	composed := &composedFields{Entity: identity, ChangeInfo: domain.ChangeInfo{CreatedAt: at, CreatedBy: by, UpdatedAt: at, UpdatedBy: by}, DeletionState: domain.DeletionState{DeletedAt: &at, DeletedBy: &by}}
	left, err := json.Marshal(tracked)
	if err != nil {
		t.Fatal(err)
	}
	right, err := json.Marshal(composed)
	if err != nil {
		t.Fatal(err)
	}
	if string(left) != string(right) {
		t.Fatalf("component wire fields changed: %s != %s", left, right)
	}
	before, err := meta.Describe[*TrackedEntity[int64]]()
	if err != nil {
		t.Fatal(err)
	}
	after, err := meta.Describe[*composedFields]()
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Fields()) != len(after.Fields()) {
		t.Fatal("embedded component metadata differs")
	}
	for _, field := range before.Fields() {
		other, err := after.Resolve(meta.ByField(field.Name))
		if err != nil {
			t.Fatal(err)
		}
		if other.Managed != field.Managed || other.Identity != field.Identity {
			t.Fatalf("metadata semantics changed for %s", field.Name)
		}
	}
}
