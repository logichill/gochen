package audited

import (
	"testing"

	"gochen/auth"
	"gochen/domain/spec"
)

func TestSoftDeleteAndRestoreRecordOperators(t *testing.T) {
	app, repo := newAuditedApplication(t, Options[*auditDoc, int64]{})
	ctx := operatorContext(t, "alice")
	doc := mustCreateDoc(t, app, ctx, "first")

	deleteCtx := operatorContext(t, "bob")
	if err := app.Delete(deleteCtx, doc.GetID()); err != nil {
		t.Fatalf("Delete(): %v", err)
	}
	deleted, err := repo.GetWithDeleted(ctx, doc.GetID(), spec.All())
	if err != nil {
		t.Fatalf("GetWithDeleted(): %v", err)
	}
	if deleted.GetDeletedBy() == nil || *deleted.GetDeletedBy() != "bob" {
		t.Fatalf("deleted_by = %v, want bob", deleted.GetDeletedBy())
	}

	restoreCtx := operatorContext(t, "carol")
	restored, err := app.Restore(restoreCtx, doc.GetID())
	if err != nil {
		t.Fatalf("Restore(): %v", err)
	}
	if restored.GetDeletedBy() != nil || restored.DeletedAt != nil {
		t.Fatalf("restored deletion fields = %v/%v, want nil", restored.GetDeletedBy(), restored.DeletedAt)
	}

	records := auditRecords(t, repo, ctx, doc.GetID())
	wantActions := []string{actionCreate, actionDelete, actionRestore}
	wantOperators := []string{"alice", "bob", "carol"}
	if len(records) != len(wantActions) {
		t.Fatalf("audit records = %d, want %d", len(records), len(wantActions))
	}
	for i := range wantActions {
		if records[i].Action != wantActions[i] || records[i].Operator != wantOperators[i] {
			t.Fatalf("record[%d] = %s/%s, want %s/%s",
				i, records[i].Action, records[i].Operator, wantActions[i], wantOperators[i])
		}
	}
}

func TestPrincipalOperatorOverridesContextOperator(t *testing.T) {
	app, repo := newAuditedApplication(t, Options[*auditDoc, int64]{})

	ctx := operatorContext(t, "legacy")
	principalCtx, err := auth.WithPrincipal(ctx, auth.Principal{SubjectID: "alice"})
	if err != nil {
		t.Fatalf("WithPrincipal(): %v", err)
	}
	doc := mustCreateDoc(t, app, principalCtx, "first")
	if records := auditRecords(t, repo, principalCtx, doc.GetID()); records[0].Operator != "alice" {
		t.Fatalf("create operator = %q, want alice", records[0].Operator)
	}

	deleteCtx, err := auth.WithPrincipal(operatorContext(t, "legacy"), auth.Principal{SubjectID: "bob"})
	if err != nil {
		t.Fatalf("WithPrincipal(): %v", err)
	}
	if err := app.Delete(deleteCtx, doc.GetID()); err != nil {
		t.Fatalf("Delete(): %v", err)
	}
	deleted, err := repo.GetWithDeleted(principalCtx, doc.GetID(), spec.All())
	if err != nil {
		t.Fatalf("GetWithDeleted(): %v", err)
	}
	if deleted.GetDeletedBy() == nil || *deleted.GetDeletedBy() != "bob" {
		t.Fatalf("deleted_by = %v, want bob", deleted.GetDeletedBy())
	}
	records := auditRecords(t, repo, principalCtx, doc.GetID())
	if records[1].Operator != "bob" {
		t.Fatalf("delete operator = %q, want bob", records[1].Operator)
	}
}
