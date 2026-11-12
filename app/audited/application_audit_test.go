package audited

import (
	"context"
	"encoding/json"
	"gochen/domain/meta"
	"strings"
	"testing"

	appcrud "gochen/app/crud"
	"gochen/auth"
	"gochen/contextx"

	domaudited "gochen/domain/audited"
	"gochen/domain/spec"
	"gochen/errors"
)

type staticAuthorizer struct {
	spec  spec.Spec
	vocab auth.Vocabulary
}

func (a staticAuthorizer) Authorize(context.Context, auth.Request) (auth.Decision, error) {
	return auth.Decision{Allowed: true, Spec: a.spec}, nil
}

func (a staticAuthorizer) Vocabulary() auth.Vocabulary { return a.vocab }

func TestCreateAuditsSystemActor(t *testing.T) {
	app, repo := newAuditedApplication(t, Options[*auditDoc, int64]{})
	base, err := auth.WithPrincipal(operatorContext(t, "legacy"), auth.Principal{SubjectID: "alice"})
	if err != nil {
		t.Fatalf("WithPrincipal(): %v", err)
	}
	ctx := auth.AsSystem(base, "nightly batch")

	doc := mustCreateDoc(t, app, ctx, "first")
	records := auditRecords(t, repo, base, doc.GetID())
	if len(records) != 1 {
		t.Fatalf("audit records = %d, want 1", len(records))
	}
	record := records[0]
	if record.Operator != "system" {
		t.Fatalf("operator = %q, want system", record.Operator)
	}
	if record.Initiator != "alice" {
		t.Fatalf("initiator = %q, want alice", record.Initiator)
	}
	if record.Reason != "nightly batch" {
		t.Fatalf("reason = %q, want nightly batch", record.Reason)
	}
}

func TestAuditCorrelationIDFromRequestID(t *testing.T) {
	app, repo := newAuditedApplication(t, Options[*auditDoc, int64]{})
	ctx, err := contextx.WithRequestID(operatorContext(t, "alice"), "req-123")
	if err != nil {
		t.Fatalf("WithRequestID(): %v", err)
	}

	doc := mustCreateDoc(t, app, ctx, "first")
	records := auditRecords(t, repo, ctx, doc.GetID())
	if records[0].CorrelationID != "req-123" {
		t.Fatalf("correlation id = %q, want req-123", records[0].CorrelationID)
	}
}

func TestAuditCorrelationIDFallsBackToTraceID(t *testing.T) {
	app, repo := newAuditedApplication(t, Options[*auditDoc, int64]{})
	ctx, err := contextx.WithTraceID(operatorContext(t, "alice"), "trace-456")
	if err != nil {
		t.Fatalf("WithTraceID(): %v", err)
	}

	doc := mustCreateDoc(t, app, ctx, "first")
	records := auditRecords(t, repo, ctx, doc.GetID())
	if records[0].CorrelationID != "trace-456" {
		t.Fatalf("correlation id = %q, want trace-456", records[0].CorrelationID)
	}
}

func TestAuditBoundaryFromEntity(t *testing.T) {
	app, repo := newAuditedApplication(t, Options[*auditDoc, int64]{})
	ctx, err := contextx.WithTenantID(operatorContext(t, "alice"), "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	doc := &auditDoc{Name: "first", Tenant: "tenant-a", Scope: 42, Owner: "owner-a"}
	if err := app.Create(ctx, doc); err != nil {
		t.Fatalf("Create(): %v", err)
	}

	records := auditRecords(t, repo, ctx, doc.GetID())
	record := records[0]
	if auditTenant(record) != "tenant-a" || auditScope(record) != 42 || auditOwner(record) != "owner-a" {
		t.Fatalf("boundary = %s/%d/%s", auditTenant(record), auditScope(record), auditOwner(record))
	}
}

func TestAuditReasonFromExecute(t *testing.T) {
	app, repo := newAuditedApplication(t, updateActionOptions("rename"))
	ctx := operatorContext(t, "alice")
	doc := mustCreateDoc(t, app, ctx, "a")

	if _, err := app.Execute(ctx, "rename", doc.GetID(), func(_ context.Context, e *auditDoc) error {
		e.Name = "b"
		return nil
	}, WithReason("manual fix")); err != nil {
		t.Fatalf("Execute(): %v", err)
	}
	records := auditRecords(t, repo, ctx, doc.GetID())
	if len(records) != 2 {
		t.Fatalf("audit records = %d, want 2", len(records))
	}
	if records[1].Action != "rename" || records[1].Reason != "manual fix" {
		t.Fatalf("record = %+v", records[1])
	}
}

func TestProjectorFiltersAuditSnapshot(t *testing.T) {
	app, repo := newAuditedApplication(t, Options[*auditDoc, int64]{
		Projector: func(e *auditDoc) (any, error) {
			return map[string]any{"name": e.Name}, nil
		},
	})
	ctx := operatorContext(t, "alice")
	doc := mustCreateDoc(t, app, ctx, "a")

	records := auditRecords(t, repo, ctx, doc.GetID())
	var snapshot map[string]any
	if err := json.Unmarshal(records[0].Changes, &snapshot); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	if len(snapshot) != 1 || snapshot["name"] != "a" {
		t.Fatalf("snapshot = %+v, want only name", snapshot)
	}

	loaded, err := app.Get(ctx, doc.GetID())
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}
	loaded.Name = "b"
	loaded.State = "active"
	if err := app.Update(ctx, loaded); err != nil {
		t.Fatalf("Update(): %v", err)
	}
	records = auditRecords(t, repo, ctx, doc.GetID())
	var diffs []domaudited.FieldChange
	if err := json.Unmarshal(records[1].Changes, &diffs); err != nil {
		t.Fatalf("decode diff: %v", err)
	}
	if len(diffs) != 1 || diffs[0].Field != "name" {
		t.Fatalf("diff = %+v, want only name", diffs)
	}
}

func TestUpdatePersistsFieldsHiddenByAuditProjector(t *testing.T) {
	app, repo := newAuditedApplication(t, Options[*auditDoc, int64]{
		Projector: func(e *auditDoc) (any, error) { return map[string]any{"name": e.Name}, nil },
	})
	ctx := operatorContext(t, "alice")
	doc := mustCreateDoc(t, app, ctx, "visible")
	loaded, err := app.Get(ctx, doc.GetID())
	if err != nil {
		t.Fatal(err)
	}
	loaded.State = "active"
	if err := app.Update(ctx, loaded); err != nil {
		t.Fatal(err)
	}
	stored, err := app.Get(ctx, doc.GetID())
	if err != nil {
		t.Fatal(err)
	}
	if stored.State != "active" || stored.Version != 1 {
		t.Fatalf("hidden business update was discarded: state=%q version=%d", stored.State, stored.Version)
	}
	records := auditRecords(t, repo, ctx, doc.GetID())
	if len(records) != 2 {
		t.Fatalf("audit records=%d, want 2", len(records))
	}
	if strings.Contains(string(records[1].Changes), "state") || strings.Contains(string(records[1].Changes), "active") {
		t.Fatalf("hidden field leaked into audit: %s", records[1].Changes)
	}
	if err := app.Update(ctx, stored); err != nil {
		t.Fatal(err)
	}
	if stored.Version != 1 || len(auditRecords(t, repo, ctx, doc.GetID())) != 2 {
		t.Fatal("unchanged entity should skip persistence and audit")
	}
}

func TestAuditTrailRejectsNonBoundarySpec(t *testing.T) {
	app, _ := newAuditedApplication(t, Options[*auditDoc, int64]{
		Options: appcrud.Options[*auditDoc, int64]{
			Authorizer: staticAuthorizer{
				spec:  spec.Eq(meta.ByField("Name"), "x"),
				vocab: auth.Vocabulary{Refs: []meta.Ref{meta.ByField("Name")}},
			},
		},
	})
	if _, err := app.AuditTrail(context.Background(), 1, 0, 10); !errors.Is(err, errors.Internal) {
		t.Fatalf("AuditTrail() = %v, want Internal", err)
	}
}

func TestAuditTrailFiltersByBoundarySpec(t *testing.T) {
	repo := newAuditRepo()
	ownerOption := func(owner string) Options[*auditDoc, int64] {
		return Options[*auditDoc, int64]{
			Options: appcrud.Options[*auditDoc, int64]{
				Authorizer: staticAuthorizer{
					spec:  spec.Eq(meta.ByDimension("owner"), owner),
					vocab: auth.Vocabulary{Refs: []meta.Ref{meta.ByDimension("owner")}},
				},
			},
		}
	}
	writer, _ := newAuditedApplicationWith(t, repo, repo, Options[*auditDoc, int64]{})
	ctx := operatorContext(t, "alice")
	doc := &auditDoc{Name: "first", Owner: "owner-a"}
	if err := writer.Create(ctx, doc); err != nil {
		t.Fatalf("Create(): %v", err)
	}

	matching, _ := newAuditedApplicationWith(t, repo, repo, ownerOption("owner-a"))
	records, err := matching.AuditTrail(ctx, doc.GetID(), 0, 10)
	if err != nil {
		t.Fatalf("AuditTrail(owner-a): %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("audit trail = %d, want 1", len(records))
	}

	other, _ := newAuditedApplicationWith(t, repo, repo, ownerOption("owner-b"))
	records, err = other.AuditTrail(ctx, doc.GetID(), 0, 10)
	if err != nil {
		t.Fatalf("AuditTrail(owner-b): %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("audit trail = %d, want 0", len(records))
	}
}

func TestAuditTrailPaginates(t *testing.T) {
	app, _ := newAuditedApplication(t, updateActionOptions("rename"))
	ctx := operatorContext(t, "alice")
	doc := mustCreateDoc(t, app, ctx, "a")
	if _, err := app.Execute(ctx, "rename", doc.GetID(), func(_ context.Context, e *auditDoc) error {
		e.Name = "b"
		return nil
	}); err != nil {
		t.Fatalf("Execute(): %v", err)
	}
	if err := app.Delete(ctx, doc.GetID()); err != nil {
		t.Fatalf("Delete(): %v", err)
	}

	records, err := app.AuditTrail(ctx, doc.GetID(), 1, 1)
	if err != nil {
		t.Fatalf("AuditTrail(): %v", err)
	}
	if len(records) != 1 || records[0].Action != "rename" {
		t.Fatalf("page = %+v, want rename record", records)
	}
}

func TestUpdateRollsBackWhenAuditStoreFails(t *testing.T) {
	repo := newAuditRepo()
	writer, _ := newAuditedApplicationWith(t, repo, repo, Options[*auditDoc, int64]{})
	ctx := operatorContext(t, "alice")
	doc := mustCreateDoc(t, writer, ctx, "original")

	store := &failingAuditStore{
		AuditedMemoryRepository: repo,
		err:                     errors.NewCode(errors.Internal, "audit store down"),
	}
	app, _ := newAuditedApplicationWith(t, repo, store, Options[*auditDoc, int64]{})

	loaded, err := app.Get(ctx, doc.GetID())
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}
	loaded.Name = "changed"
	if err := app.Update(ctx, loaded); !errors.Is(err, errors.Internal) {
		t.Fatalf("Update() = %v, want Internal", err)
	}
	stored, err := app.Get(ctx, doc.GetID())
	if err != nil || stored.Name != "original" || stored.GetVersion() != 0 {
		t.Fatalf("stored = %+v, %v", stored, err)
	}
	if records := auditRecords(t, repo, ctx, doc.GetID()); len(records) != 1 {
		t.Fatalf("audit records = %d, want 1", len(records))
	}
}

func TestDeleteRollsBackWhenAuditStoreFails(t *testing.T) {
	repo := newAuditRepo()
	writer, _ := newAuditedApplicationWith(t, repo, repo, Options[*auditDoc, int64]{})
	ctx := operatorContext(t, "alice")
	doc := mustCreateDoc(t, writer, ctx, "first")

	store := &failingAuditStore{
		AuditedMemoryRepository: repo,
		err:                     errors.NewCode(errors.Internal, "audit store down"),
	}
	app, _ := newAuditedApplicationWith(t, repo, store, Options[*auditDoc, int64]{})

	if err := app.Delete(ctx, doc.GetID()); !errors.Is(err, errors.Internal) {
		t.Fatalf("Delete() = %v, want Internal", err)
	}
	stored, err := repo.GetWithDeleted(ctx, doc.GetID(), spec.All())
	if err != nil {
		t.Fatalf("GetWithDeleted(): %v", err)
	}
	if stored.IsDeleted() {
		t.Fatal("delete survived audit failure")
	}
	if records := auditRecords(t, repo, ctx, doc.GetID()); len(records) != 1 {
		t.Fatalf("audit records = %d, want 1", len(records))
	}
}

func TestRestoreRollsBackWhenAuditStoreFails(t *testing.T) {
	repo := newAuditRepo()
	writer, _ := newAuditedApplicationWith(t, repo, repo, Options[*auditDoc, int64]{})
	ctx := operatorContext(t, "alice")
	doc := mustCreateDoc(t, writer, ctx, "first")
	if err := writer.Delete(ctx, doc.GetID()); err != nil {
		t.Fatalf("Delete(): %v", err)
	}

	store := &failingAuditStore{
		AuditedMemoryRepository: repo,
		err:                     errors.NewCode(errors.Internal, "audit store down"),
	}
	app, _ := newAuditedApplicationWith(t, repo, store, Options[*auditDoc, int64]{})

	if _, err := app.Restore(ctx, doc.GetID()); !errors.Is(err, errors.Internal) {
		t.Fatalf("Restore() = %v, want Internal", err)
	}
	stored, err := repo.GetWithDeleted(ctx, doc.GetID(), spec.All())
	if err != nil {
		t.Fatalf("GetWithDeleted(): %v", err)
	}
	if !stored.IsDeleted() {
		t.Fatal("restore survived audit failure")
	}
	if records := auditRecords(t, repo, ctx, doc.GetID()); len(records) != 2 {
		t.Fatalf("audit records = %d, want 2", len(records))
	}
}

func TestUpdateRejectsUnmarshalableSnapshot(t *testing.T) {
	app, repo := newAuditedApplication(t, Options[*auditDoc, int64]{})
	ctx := operatorContext(t, "alice")
	doc := mustCreateDoc(t, app, ctx, "first")

	loaded, err := app.Get(ctx, doc.GetID())
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}
	loaded.Extra = failingAuditValue("unserializable")
	if err := app.Update(ctx, loaded); !errors.Is(err, errors.Internal) {
		t.Fatalf("Update() = %v, want Internal", err)
	}
	stored, err := app.Get(ctx, doc.GetID())
	if err != nil || stored.Name != "first" || stored.GetVersion() != 0 {
		t.Fatalf("stored = %+v, %v", stored, err)
	}
	if records := auditRecords(t, repo, ctx, doc.GetID()); len(records) != 1 {
		t.Fatalf("audit records = %d, want 1", len(records))
	}
}
