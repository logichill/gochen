package deletion

import (
	"context"
	"testing"
	"time"

	appcrud "gochen/app/crud"
	"gochen/contextx"
	"gochen/domain/spec"
	"gochen/errors"
)

func TestDeleteSoftDeletesAndWritesAudit(t *testing.T) {
	app, repo := newAuditedApplication(t, testOptions[*auditDoc, int64]{})
	ctx := operatorContext(t, "alice")
	doc := mustCreateDoc(t, app, ctx, "first")

	deleteCtx := operatorContext(t, "bob")
	if err := app.Delete(deleteCtx, doc.GetID()); err != nil {
		t.Fatalf("Delete(): %v", err)
	}
	if _, err := app.Get(ctx, doc.GetID()); !errors.Is(err, errors.NotFound) {
		t.Fatalf("Get() after delete = %v, want NotFound", err)
	}
	stored, err := repo.GetWithDeleted(ctx, doc.GetID(), spec.All())
	if err != nil {
		t.Fatalf("GetWithDeleted(): %v", err)
	}
	if !stored.IsDeleted() || stored.DeletedAt == nil {
		t.Fatalf("stored entity is not soft deleted: %+v", stored.TrackedEntity)
	}
	if stored.GetDeletedBy() == nil || *stored.GetDeletedBy() != "bob" {
		t.Fatalf("deleted_by = %v, want bob", stored.GetDeletedBy())
	}
	if stored.GetVersion() != 1 {
		t.Fatalf("version after delete = %d, want 1", stored.GetVersion())
	}

	records := auditRecords(t, repo, ctx, doc.GetID())
	if len(records) != 2 {
		t.Fatalf("audit records = %d, want 2", len(records))
	}
	record := records[1]
	if record.Action != actionDelete || record.Operator != "bob" {
		t.Fatalf("delete record = %+v", record)
	}
	if record.Changes != nil {
		t.Fatalf("delete changes = %s, want nil", string(record.Changes))
	}

	visible, err := app.List(ctx, 0, 10)
	if err != nil || len(visible) != 0 {
		t.Fatalf("List() after delete = %d, %v", len(visible), err)
	}
	deleted, err := app.ListDeleted(ctx, 0, 10)
	if err != nil || len(deleted) != 1 || !deleted[0].IsDeleted() {
		t.Fatalf("ListDeleted() = %d, %v", len(deleted), err)
	}
}

func TestDeleteAlreadyDeletedReturnsNotFound(t *testing.T) {
	app, repo := newAuditedApplication(t, testOptions[*auditDoc, int64]{})
	ctx := operatorContext(t, "alice")
	doc := mustCreateDoc(t, app, ctx, "first")

	if err := app.Delete(ctx, doc.GetID()); err != nil {
		t.Fatalf("Delete(): %v", err)
	}
	if err := app.Delete(ctx, doc.GetID()); !errors.Is(err, errors.NotFound) {
		t.Fatalf("second Delete() = %v, want NotFound", err)
	}
	if records := auditRecords(t, repo, ctx, doc.GetID()); len(records) != 2 {
		t.Fatalf("audit records = %d, want 2", len(records))
	}
}

func TestDeleteMissingReturnsNotFound(t *testing.T) {
	app, _ := newAuditedApplication(t, testOptions[*auditDoc, int64]{})
	ctx := operatorContext(t, "alice")
	if err := app.Delete(ctx, 42); !errors.Is(err, errors.NotFound) {
		t.Fatalf("Delete(42) = %v, want NotFound", err)
	}
}

func TestRestoreNotDeletedReturnsConflict(t *testing.T) {
	app, repo := newAuditedApplication(t, testOptions[*auditDoc, int64]{})
	ctx := operatorContext(t, "alice")
	doc := mustCreateDoc(t, app, ctx, "first")

	if _, err := app.Restore(ctx, doc.GetID()); !errors.Is(err, errors.Conflict) {
		t.Fatalf("Restore() = %v, want Conflict", err)
	}
	if records := auditRecords(t, repo, ctx, doc.GetID()); len(records) != 1 {
		t.Fatalf("audit records = %d, want 1", len(records))
	}
}

func TestRestoreReturnsCommittedEntityWhenPostCommitFails(t *testing.T) {
	want := errors.New("restore callback failed")
	app, repo := newAuditedApplication(t, testOptions[*auditDoc, int64]{
		Options: appcrud.Options[*auditDoc, int64]{
			Hooks: appcrud.Hooks[*auditDoc]{
				actionRestore: appcrud.Stage[*auditDoc]{
					PostCommit: func(context.Context, *auditDoc) error { return want },
				},
			},
		},
	})
	ctx := operatorContext(t, "alice")
	doc := mustCreateDoc(t, app, ctx, "restored")
	if err := app.Delete(ctx, doc.GetID()); err != nil {
		t.Fatal(err)
	}
	result, err := app.Restore(ctx, doc.GetID())
	if !contextx.IsAfterCommitError(err) || !errors.Is(err, want) {
		t.Fatalf("Restore() = %v, want committed callback failure", err)
	}
	if result == nil || result.IsDeleted() || result.GetVersion() != 2 {
		t.Fatalf("Restore() lost committed state: %+v", result)
	}
	stored, err := app.Get(ctx, doc.GetID())
	if err != nil || stored.IsDeleted() || stored.GetVersion() != result.GetVersion() {
		t.Fatalf("restored state: %+v, %v", stored, err)
	}
	if records := auditRecords(t, repo, ctx, doc.GetID()); len(records) != 3 || records[2].Action != actionRestore {
		t.Fatalf("restore audit was not committed: %+v", records)
	}
}

func TestRestoreMissingReturnsNotFound(t *testing.T) {
	app, _ := newAuditedApplication(t, testOptions[*auditDoc, int64]{})
	ctx := operatorContext(t, "alice")
	if _, err := app.Restore(ctx, 42); !errors.Is(err, errors.NotFound) {
		t.Fatalf("Restore(42) = %v, want NotFound", err)
	}
}

func TestRestoreClearsDeletionAndWritesAudit(t *testing.T) {
	app, repo := newAuditedApplication(t, testOptions[*auditDoc, int64]{})
	ctx := operatorContext(t, "alice")
	doc := mustCreateDoc(t, app, ctx, "first")
	if err := app.Delete(ctx, doc.GetID()); err != nil {
		t.Fatalf("Delete(): %v", err)
	}

	restoreCtx := operatorContext(t, "carol")
	restored, err := app.Restore(restoreCtx, doc.GetID(), WithReason("mistaken delete"))
	if err != nil {
		t.Fatalf("Restore(): %v", err)
	}
	if restored.IsDeleted() || restored.DeletedAt != nil {
		t.Fatalf("restored entity still deleted: %+v", restored.TrackedEntity)
	}
	if restored.GetDeletedBy() != nil {
		t.Fatalf("deleted_by = %v, want nil", restored.GetDeletedBy())
	}
	if restored.GetVersion() != 2 {
		t.Fatalf("version after restore = %d, want 2", restored.GetVersion())
	}
	stored, err := app.Get(ctx, doc.GetID())
	if err != nil || stored.Name != "first" {
		t.Fatalf("Get() after restore = %+v, %v", stored, err)
	}

	records := auditRecords(t, repo, ctx, doc.GetID())
	if len(records) != 3 {
		t.Fatalf("audit records = %d, want 3", len(records))
	}
	record := records[2]
	if record.Action != actionRestore || record.Operator != "carol" {
		t.Fatalf("restore record = %+v", record)
	}
	if record.Reason != "mistaken delete" {
		t.Fatalf("restore reason = %q", record.Reason)
	}
	if record.Changes != nil {
		t.Fatalf("restore changes = %s, want nil", string(record.Changes))
	}
	deleted, err := app.ListDeleted(ctx, 0, 10)
	if err != nil || len(deleted) != 0 {
		t.Fatalf("ListDeleted() after restore = %d, %v", len(deleted), err)
	}
}

func TestRestoreUsesDedicatedHooks(t *testing.T) {
	var updateAfter, restoreBefore, restoreAfter int
	app, _ := newAuditedApplication(t, testOptions[*auditDoc, int64]{
		Options: appcrud.Options[*auditDoc, int64]{
			Hooks: appcrud.Hooks[*auditDoc]{
				appcrud.ActionUpdate: {
					After: func(context.Context, *auditDoc) error {
						updateAfter++
						return nil
					},
				},
				actionRestore: {
					Before: func(context.Context, *auditDoc) error {
						restoreBefore++
						return nil
					},
					After: func(context.Context, *auditDoc) error {
						restoreAfter++
						return nil
					},
				},
			},
		},
	})
	ctx := operatorContext(t, "alice")
	doc := mustCreateDoc(t, app, ctx, "first")
	if err := app.Delete(ctx, doc.GetID()); err != nil {
		t.Fatalf("Delete(): %v", err)
	}
	if _, err := app.Restore(ctx, doc.GetID()); err != nil {
		t.Fatalf("Restore(): %v", err)
	}
	if updateAfter != 0 {
		t.Fatalf("update hook ran %d times on restore", updateAfter)
	}
	if restoreBefore != 1 || restoreAfter != 1 {
		t.Fatalf("restore hooks = %d/%d, want 1/1", restoreBefore, restoreAfter)
	}
}

func TestPurgeUsesDedicatedHooks(t *testing.T) {
	var deleteAfter, purgeBefore, purgeAfter int
	app, repo := newAuditedApplication(t, testOptions[*auditDoc, int64]{
		Options: appcrud.Options[*auditDoc, int64]{
			Hooks: appcrud.Hooks[*auditDoc]{
				appcrud.ActionDelete: {
					After: func(context.Context, *auditDoc) error {
						deleteAfter++
						return nil
					},
				},
				actionPurge: {
					Before: func(context.Context, *auditDoc) error {
						purgeBefore++
						return nil
					},
					After: func(context.Context, *auditDoc) error {
						purgeAfter++
						return nil
					},
				},
			},
		},
	})
	ctx := operatorContext(t, "alice")
	doc := mustCreateDoc(t, app, ctx, "first")
	if err := app.Delete(ctx, doc.GetID()); err != nil {
		t.Fatalf("Delete(): %v", err)
	}
	if err := app.Purge(ctx, doc.GetID(), WithReason("retention")); err != nil {
		t.Fatalf("Purge(): %v", err)
	}
	if deleteAfter != 1 {
		t.Fatalf("delete hook ran %d times, want 1", deleteAfter)
	}
	if purgeBefore != 1 || purgeAfter != 1 {
		t.Fatalf("purge hooks = %d/%d, want 1/1", purgeBefore, purgeAfter)
	}
	if records := auditRecords(t, repo, ctx, doc.GetID()); len(records) != 3 {
		t.Fatalf("audit records = %d, want 3", len(records))
	}
}

func TestPurgeRemovesActiveEntity(t *testing.T) {
	app, repo := newAuditedApplication(t, testOptions[*auditDoc, int64]{})
	ctx := operatorContext(t, "alice")
	doc := mustCreateDoc(t, app, ctx, "first")

	if err := app.Purge(ctx, doc.GetID()); err != nil {
		t.Fatalf("Purge(): %v", err)
	}
	if _, err := repo.GetWithDeleted(ctx, doc.GetID(), spec.All()); !errors.Is(err, errors.NotFound) {
		t.Fatalf("GetWithDeleted() after purge = %v, want NotFound", err)
	}
	records := auditRecords(t, repo, ctx, doc.GetID())
	if len(records) != 2 || records[1].Action != actionPurge {
		t.Fatalf("audit records = %+v", records)
	}
}

func TestPurgeMissingReturnsNotFound(t *testing.T) {
	app, _ := newAuditedApplication(t, testOptions[*auditDoc, int64]{})
	ctx := operatorContext(t, "alice")
	if err := app.Purge(ctx, 42); !errors.Is(err, errors.NotFound) {
		t.Fatalf("Purge(42) = %v, want NotFound", err)
	}
}

func TestAuditTrailSurvivesPurge(t *testing.T) {
	app, _ := newAuditedApplication(t, testOptions[*auditDoc, int64]{})
	ctx := operatorContext(t, "alice")
	doc := mustCreateDoc(t, app, ctx, "first")
	if err := app.Delete(ctx, doc.GetID()); err != nil {
		t.Fatalf("Delete(): %v", err)
	}
	if err := app.Purge(ctx, doc.GetID()); err != nil {
		t.Fatalf("Purge(): %v", err)
	}

	trail, err := app.AuditTrail(ctx, doc.GetID(), 0, 10)
	if err != nil {
		t.Fatalf("AuditTrail(): %v", err)
	}
	if len(trail) != 3 {
		t.Fatalf("audit trail = %d records, want 3", len(trail))
	}
	want := []string{actionCreate, actionDelete, actionPurge}
	for i, action := range want {
		if trail[i].Action != action {
			t.Fatalf("trail[%d].Action = %q, want %q", i, trail[i].Action, action)
		}
	}
	count, err := app.Count(ctx)
	if err != nil || count != 0 {
		t.Fatalf("Count() after purge = %d, %v", count, err)
	}
}

func TestSoftDeleteTimestampIsRecorded(t *testing.T) {
	app, repo := newAuditedApplication(t, testOptions[*auditDoc, int64]{})
	ctx := operatorContext(t, "alice")
	doc := mustCreateDoc(t, app, ctx, "first")

	before := time.Now()
	if err := app.Delete(ctx, doc.GetID()); err != nil {
		t.Fatalf("Delete(): %v", err)
	}
	after := time.Now()

	stored, err := repo.GetWithDeleted(ctx, doc.GetID(), spec.All())
	if err != nil {
		t.Fatalf("GetWithDeleted(): %v", err)
	}
	if stored.DeletedAt == nil || stored.DeletedAt.Before(before) || stored.DeletedAt.After(after) {
		t.Fatalf("deleted_at = %v, want within [%v, %v]", stored.DeletedAt, before, after)
	}
}
