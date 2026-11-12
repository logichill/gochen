package crud

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"gochen/auth"
	domaincrud "gochen/domain/crud"
	"gochen/domain/spec"
	"gochen/errors"
	"gochen/testkit"
)

type auditFailingEntity struct {
	domaincrud.Entity[int64]
	Name string `json:"name"`
}

func (e *auditFailingEntity) MarshalJSON() ([]byte, error) {
	if strings.HasPrefix(e.Name, "unserializable") {
		return nil, fmt.Errorf("entity audit serialization failed")
	}
	type plain auditFailingEntity
	return json.Marshal((*plain)(e))
}

func TestAuditDisabledUpdatesSkipAuditSerialization(t *testing.T) {
	for _, mode := range []string{"update", "execute", "bulk", "no-op"} {
		for _, initial := range []string{"before", "unserializable before"} {
			t.Run(mode+"/"+initial, func(t *testing.T) {
				ctx := context.Background()
				repo, err := testkit.NewMemoryRepository[*auditFailingEntity, int64](testkit.NewInt64Sequence(1))
				if err != nil {
					t.Fatal(err)
				}
				application, err := NewApplication(repo, Options[*auditFailingEntity, int64]{Resource: "document", Authorizer: auth.Allow()})
				if err != nil {
					t.Fatal(err)
				}
				entity := &auditFailingEntity{Name: initial}
				if err := application.Create(ctx, entity); err != nil {
					t.Fatal(err)
				}
				change := func(_ context.Context, e *auditFailingEntity) error {
					e.Name = "unserializable after"
					return nil
				}
				switch mode {
				case "update":
					entity.Name = "unserializable after"
					err = application.Update(ctx, entity)
				case "execute":
					_, err = application.Execute(ctx, ActionUpdate, entity.ID, change)
				case "bulk":
					_, err = application.ExecuteBulk(ctx, ActionUpdate, []int64{entity.ID}, change)
				case "no-op":
					_, err = application.Execute(ctx, ActionUpdate, entity.ID, func(context.Context, *auditFailingEntity) error { return nil })
				}
				if err != nil {
					t.Fatalf("audit-disabled %s failed: %v", mode, err)
				}
				stored, err := repo.Get(ctx, entity.ID, spec.All())
				if err != nil {
					t.Fatal(err)
				}
				wantName, wantVersion := "unserializable after", uint64(1)
				if mode == "no-op" {
					wantName, wantVersion = initial, 0
				}
				if stored.Name != wantName || stored.Version != wantVersion {
					t.Fatalf("stored = %+v, want name=%q version=%d", stored, wantName, wantVersion)
				}
			})
		}
	}
}

func TestAuditDisabledUpdateRetainsBusinessSnapshotValidation(t *testing.T) {
	ctx := context.Background()
	type businessEntity struct {
		domaincrud.Entity[int64]
		Extra any `json:"extra"`
	}
	repo, err := testkit.NewMemoryRepository[*businessEntity, int64](testkit.NewInt64Sequence(1))
	if err != nil {
		t.Fatal(err)
	}
	application, err := NewApplication(repo, Options[*businessEntity, int64]{Resource: "document", Authorizer: auth.Allow()})
	if err != nil {
		t.Fatal(err)
	}
	entity := &businessEntity{Extra: "before"}
	if err := application.Create(ctx, entity); err != nil {
		t.Fatal(err)
	}
	_, err = application.Execute(ctx, ActionUpdate, entity.ID, func(_ context.Context, e *businessEntity) error {
		e.Extra = make(chan int)
		return nil
	})
	if !errors.Is(err, errors.Unsupported) {
		t.Fatalf("expected business snapshot failure, got %v", err)
	}
	stored, err := repo.Get(ctx, entity.ID, spec.All())
	if err != nil {
		t.Fatal(err)
	}
	if stored.Extra != "before" || stored.Version != 0 {
		t.Fatalf("failed command persisted: %+v", stored)
	}
}
