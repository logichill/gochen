package deletion

import (
	"context"
	"gochen/app"
	appaudit "gochen/app/audit"
	appcrud "gochen/app/crud"
	"gochen/audit"
	"gochen/auth"
	"gochen/contextx"
	"gochen/domain"
	domaincrud "gochen/domain/crud"
	"gochen/errors"
	"gochen/testkit"
	"testing"
)

type draft struct {
	domaincrud.Entity[int64]
	domain.DeletionState
	Name string `json:"name"`
}
type approval struct {
	domaincrud.Entity[int64]
	Name string `json:"name"`
}
type failingAudit struct{ app.IAuditRepository }

func (f failingAudit) SaveAuditRecord(context.Context, audit.AuditRecord) (int64, error) {
	return 0, errors.NewCode(errors.Internal, "audit failed")
}

func TestDeletionWithoutAuditOrMutationFields(t *testing.T) {
	repo, err := testkit.NewMemoryRepository[*draft, int64](testkit.NewInt64Sequence(1))
	if err != nil {
		t.Fatal(err)
	}
	options := appcrud.Options[*draft, int64]{Resource: "draft", Authorizer: auth.Allow()}
	resource, err := appcrud.NewApplication(repo, options)
	if err != nil {
		t.Fatal(err)
	}
	trash, err := NewApplication(repo, Options[*draft, int64]{Options: options})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	entity := &draft{Name: "draft"}
	if err := resource.Create(ctx, entity); err != nil {
		t.Fatal(err)
	}
	if _, ok := resource.Descriptor().PermissionFor("delete"); ok {
		t.Fatal("resource declares deletion without its capability")
	}
	if err := resource.Delete(ctx, entity.ID); !errors.Is(err, errors.Forbidden) {
		t.Fatalf("resource delete must fail safely: %v", err)
	}
	original := entity.Version
	if err := trash.Delete(ctx, entity.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := resource.Get(ctx, entity.ID); !errors.Is(err, errors.NotFound) {
		t.Fatalf("deleted entity remains visible: %v", err)
	}
	items, err := trash.ListDeleted(ctx, 0, 10)
	if err != nil || len(items) != 1 {
		t.Fatalf("deleted list: %v %v", items, err)
	}
	if items[0].Version <= original || items[0].DeletedBy != nil {
		t.Fatal("deletion must advance version without requiring an audit actor")
	}
	restored, err := trash.Restore(ctx, entity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.IsDeleted() || restored.Version <= items[0].Version {
		t.Fatal("restore state/version invalid")
	}
	if _, err := trash.Restore(ctx, entity.ID); !errors.Is(err, errors.Conflict) {
		t.Fatalf("second restore must conflict: %v", err)
	}
	if err := trash.Delete(ctx, entity.ID); err != nil {
		t.Fatal(err)
	}
	if err := trash.Purge(ctx, entity.ID); err != nil {
		t.Fatal(err)
	}
}

func TestAuditWithoutDeletionAndRollbackOnFailure(t *testing.T) {
	repo, err := testkit.NewAuditedMemoryRepository[*approval, int64](testkit.NewInt64Sequence(1))
	if err != nil {
		t.Fatal(err)
	}
	options := appcrud.Options[*approval, int64]{Resource: "approval", Authorizer: auth.Allow(), Audit: repo}
	resource, err := appcrud.NewApplication(repo, options)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := contextx.WithOperator(context.Background(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	entity := &approval{Name: "approved"}
	if err := resource.Create(ctx, entity); err != nil {
		t.Fatal(err)
	}
	reader, err := appaudit.NewReader[int64](repo, repo, appaudit.Options{Resource: "approval", Authorizer: auth.Allow()})
	if err != nil {
		t.Fatal(err)
	}
	records, err := reader.AuditTrail(ctx, entity.ID, 0, 10)
	if err != nil || len(records) != 1 {
		t.Fatalf("history: %v %v", records, err)
	}
	options.Audit = failingAudit{repo}
	failing, err := appcrud.NewApplication(repo, options)
	if err != nil {
		t.Fatal(err)
	}
	version := entity.Version
	entity.Name = "must roll back"
	if err := failing.Update(ctx, entity); err == nil {
		t.Fatal("expected audit failure")
	}
	stored, err := resource.Get(ctx, entity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Name != "approved" || stored.Version != version {
		t.Fatalf("failed audit committed entity: %+v", stored)
	}
	if err := resource.Delete(ctx, entity.ID); err != nil {
		t.Fatal(err)
	}
	records, err = reader.AuditTrail(ctx, entity.ID, 0, 10)
	if err != nil || len(records) != 2 {
		t.Fatalf("history after physical deletion: %v %v", records, err)
	}
	readOnly, err := appcrud.NewReadApplication(repo, options)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := any(readOnly).(appaudit.IAuditReader[int64]); ok {
		t.Fatal("resource read type exposes optional audit")
	}
	if _, ok := any(readOnly).(appcrud.IWriteApplication[*approval, int64]); ok {
		t.Fatal("read type exposes writes")
	}
}

func TestDeletionWithoutAuditPreservesAvailableOperator(t *testing.T) {
	repo, err := testkit.NewMemoryRepository[*draft, int64](testkit.NewInt64Sequence(1))
	if err != nil {
		t.Fatal(err)
	}
	options := appcrud.Options[*draft, int64]{Resource: "draft", Authorizer: auth.Allow()}
	resource, err := appcrud.NewApplication(repo, options)
	if err != nil {
		t.Fatal(err)
	}
	trash, err := NewApplication(repo, Options[*draft, int64]{Options: options})
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := contextx.WithOperator(context.Background(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	entity := &draft{Name: "draft"}
	if err := resource.Create(ctx, entity); err != nil {
		t.Fatal(err)
	}
	if err := trash.Delete(ctx, entity.ID); err != nil {
		t.Fatal(err)
	}
	deleted, err := trash.ListDeleted(ctx, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 1 || deleted[0].DeletedBy == nil || *deleted[0].DeletedBy != "alice" {
		t.Fatalf("lost available attribution: %+v", deleted)
	}
	restored, err := trash.Restore(ctx, entity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.IsDeleted() || restored.DeletedBy != nil {
		t.Fatal("restore must clear deletion attribution")
	}
}
