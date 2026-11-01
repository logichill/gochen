package audited

import (
	"context"
	"testing"

	"gochen/auth"
	"gochen/contextx"
	"gochen/domain/spec"
	"gochen/errors"
)

func TestPurgeRequiresOperator(t *testing.T) {
	app, repo := newAuditedApplication(t, Options[*auditDoc, int64]{})
	ctx := operatorContext(t, "alice")
	doc := mustCreateDoc(t, app, ctx, "first")

	if err := app.Purge(context.Background(), doc.GetID()); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("Purge() without operator = %v, want InvalidInput", err)
	}
	if _, err := app.Get(ctx, doc.GetID()); err != nil {
		t.Fatalf("entity removed despite rejected purge: %v", err)
	}
	if records := auditRecords(t, repo, ctx, doc.GetID()); len(records) != 1 {
		t.Fatalf("audit records = %d, want 1", len(records))
	}
}

func TestPurgeWritesAuditWithReasonAndBoundary(t *testing.T) {
	app, repo := newAuditedApplication(t, Options[*auditDoc, int64]{})
	ctx, err := contextx.WithTenantID(operatorContext(t, "alice"), "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	doc := &auditDoc{Name: "first", State: "new", Tenant: "tenant-a", Scope: 42, Owner: "owner-a"}
	if err := app.Create(ctx, doc); err != nil {
		t.Fatalf("Create(): %v", err)
	}
	if err := app.Delete(ctx, doc.GetID()); err != nil {
		t.Fatalf("Delete(): %v", err)
	}

	purgeCtx, err := contextx.WithOperator(ctx, "dave")
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Purge(purgeCtx, doc.GetID(), WithReason("retention")); err != nil {
		t.Fatalf("Purge(): %v", err)
	}
	if _, err := repo.GetWithDeleted(ctx, doc.GetID(), spec.All()); !errors.Is(err, errors.NotFound) {
		t.Fatalf("GetWithDeleted() after purge = %v, want NotFound", err)
	}
	if _, err := app.Restore(ctx, doc.GetID()); !errors.Is(err, errors.NotFound) {
		t.Fatalf("Restore() after purge = %v, want NotFound", err)
	}

	records := auditRecords(t, repo, ctx, doc.GetID())
	if len(records) != 3 {
		t.Fatalf("audit records = %d, want 3", len(records))
	}
	record := records[2]
	if record.Action != actionPurge || record.Operator != "dave" {
		t.Fatalf("purge record = %+v", record)
	}
	if record.Reason != "retention" {
		t.Fatalf("purge reason = %q, want retention", record.Reason)
	}
	if record.Changes != nil {
		t.Fatalf("purge changes = %s, want nil", string(record.Changes))
	}
	if record.TenantID != "tenant-a" || record.ScopeID != 42 || record.OwnerID != "owner-a" {
		t.Fatalf("purge boundary = %s/%d/%s", record.TenantID, record.ScopeID, record.OwnerID)
	}
	if trail, err := app.AuditTrail(ctx, doc.GetID(), 0, 10); err != nil || len(trail) != 3 {
		t.Fatalf("AuditTrail() = %d, %v", len(trail), err)
	}
}

func TestPurgeBySystemActorRecordsInitiator(t *testing.T) {
	app, repo := newAuditedApplication(t, Options[*auditDoc, int64]{})
	base, err := auth.WithPrincipal(operatorContext(t, "legacy"), auth.Principal{SubjectID: "alice"})
	if err != nil {
		t.Fatalf("WithPrincipal(): %v", err)
	}
	doc := mustCreateDoc(t, app, base, "first")

	ctx := auth.AsSystem(base, "retention job")
	if err := app.Purge(ctx, doc.GetID()); err != nil {
		t.Fatalf("Purge(): %v", err)
	}
	records := auditRecords(t, repo, base, doc.GetID())
	record := records[len(records)-1]
	if record.Action != actionPurge {
		t.Fatalf("last action = %q, want purge", record.Action)
	}
	if record.Operator != "system" || record.Initiator != "alice" || record.Reason != "retention job" {
		t.Fatalf("system actor = %s/%s/%s", record.Operator, record.Initiator, record.Reason)
	}
}
