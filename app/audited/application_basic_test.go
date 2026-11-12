package audited

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	appcrud "gochen/app/crud"
	"gochen/auth"
	domaudited "gochen/domain/audited"
	"gochen/errors"
)

func TestNewApplicationValidatesPorts(t *testing.T) {
	repo := newAuditRepo()
	valid := Options[*auditDoc, int64]{Resource: "doc", Authorizer: auth.Allow()}

	if _, err := NewApplication[*auditDoc, int64](nil, repo, valid); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("NewApplication(nil repo) = %v, want InvalidInput", err)
	}
	if _, err := NewApplication[*auditDoc, int64](repo, nil, valid); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("NewApplication(nil audit store) = %v, want InvalidInput", err)
	}
	if _, err := NewApplication[*auditDoc, int64](repo, saveOnlyAuditStore{}, valid); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("NewApplication(store without query port) = %v, want InvalidInput", err)
	}
	if _, err := NewApplication[*auditDoc, int64](repo, otherSourceAuditStore{}, valid); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("NewApplication(data source mismatch) = %v, want InvalidInput", err)
	}
	if _, err := NewApplication[*auditDoc, int64](repo, repo, Options[*auditDoc, int64]{
		Authorizer: auth.Allow(),
	}); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("NewApplication(empty resource) = %v, want InvalidInput", err)
	}
}

func TestDescriptorListsLifecycleActions(t *testing.T) {
	app, _ := newAuditedApplication(t, Options[*auditDoc, int64]{})
	names := make(map[string]bool)
	for _, action := range app.Descriptor().Actions {
		names[action.Name] = true
	}
	want := []string{
		"read", "list", "create", "update", "delete",
		"restore", "purge", "audit_read", "list_deleted",
	}
	if len(app.Descriptor().Actions) != len(want) {
		t.Fatalf("actions = %+v", app.Descriptor().Actions)
	}
	for _, name := range want {
		if !names[name] {
			t.Fatalf("descriptor missing action %q", name)
		}
	}
}

func TestActionPermissionsOverrideLifecycleActions(t *testing.T) {
	app, _ := newAuditedApplication(t, Options[*auditDoc, int64]{
		Options: appcrud.Options[*auditDoc, int64]{
			ActionPermissions: map[string]string{
				"create":       "doc:api:manage",
				"restore":      "doc:api:manage",
				"purge":        "doc:api:manage",
				"list_deleted": "doc:api:manage",
			},
		},
	})
	for _, name := range []string{"create", "restore", "purge", "list_deleted"} {
		permission, ok := app.Descriptor().PermissionFor(name)
		if !ok || permission != "doc:api:manage" {
			t.Fatalf("%s permission = %q, %v; want doc:api:manage", name, permission, ok)
		}
	}
	if permission, _ := app.Descriptor().PermissionFor("read"); permission != "doc:api:read" {
		t.Fatalf("read permission = %q; want default", permission)
	}
}

func TestActionPermissionsRejectUnknownAction(t *testing.T) {
	repo := newAuditRepo()
	_, err := NewApplication[*auditDoc, int64](repo, repo, Options[*auditDoc, int64]{
		Options: appcrud.Options[*auditDoc, int64]{
			Resource:          "doc",
			Authorizer:        auth.Allow(),
			ActionPermissions: map[string]string{"import": "doc:api:manage"},
		},
	})
	if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("NewApplication(unknown action override) = %v, want InvalidInput", err)
	}
}

func TestCreateRequiresOperator(t *testing.T) {
	app, _ := newAuditedApplication(t, Options[*auditDoc, int64]{})
	err := app.Create(context.Background(), &auditDoc{Name: "a"})
	requireErrorCode(t, err, errors.InvalidInput)
	count, err := app.Count(context.Background())
	if err != nil || count != 0 {
		t.Fatalf("audit write without operator must not persist, Count() = %d, %v", count, err)
	}
}

func TestCreatePersistsAndWritesAudit(t *testing.T) {
	app, repo := newAuditedApplication(t, Options[*auditDoc, int64]{})
	ctx := operatorContext(t, "alice")
	entity := &auditDoc{Name: "first", State: "new"}
	before := time.Now()

	if err := app.Create(ctx, entity); err != nil {
		t.Fatalf("Create(): %v", err)
	}
	after := time.Now()

	stored, err := app.Get(ctx, entity.GetID())
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}
	if stored.Name != "first" || stored.State != "new" || stored.GetVersion() != 0 {
		t.Fatalf("stored = %+v", stored)
	}

	records := auditRecords(t, repo, ctx, entity.GetID())
	if len(records) != 1 {
		t.Fatalf("audit records = %d, want 1", len(records))
	}
	record := records[0]
	if record.Action != "create" || record.Operator != "alice" {
		t.Fatalf("record = %+v", record)
	}
	if record.ResourceKind != "doc" || record.EntityID != strconv.FormatInt(entity.GetID(), 10) {
		t.Fatalf("record boundary = %+v", record)
	}
	if record.Timestamp.Before(before) || record.Timestamp.After(after) {
		t.Fatalf("timestamp = %v, want within [%v, %v]", record.Timestamp, before, after)
	}
	var snapshot map[string]any
	if err := json.Unmarshal(record.Changes, &snapshot); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	if snapshot["name"] != "first" || snapshot["state"] != "new" {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}

func TestUpdateWritesBusinessFieldDiff(t *testing.T) {
	app, repo := newAuditedApplication(t, Options[*auditDoc, int64]{})
	ctx := operatorContext(t, "alice")
	created := mustCreateDoc(t, app, ctx, "old")

	loaded, err := app.Get(ctx, created.GetID())
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}
	loaded.Name = "new"
	loaded.State = "active"
	if err := app.Update(ctx, loaded); err != nil {
		t.Fatalf("Update(): %v", err)
	}

	records := auditRecords(t, repo, ctx, created.GetID())
	if len(records) != 2 {
		t.Fatalf("audit records = %d, want 2", len(records))
	}
	if records[1].Action != "update" {
		t.Fatalf("action = %q, want update", records[1].Action)
	}
	var diffs []domaudited.FieldChange
	if err := json.Unmarshal(records[1].Changes, &diffs); err != nil {
		t.Fatalf("decode diff: %v", err)
	}
	changes := make(map[string]domaudited.FieldChange, len(diffs))
	for _, diff := range diffs {
		changes[diff.Field] = diff
	}
	if len(changes) != 2 {
		t.Fatalf("expected only business fields, got %+v", diffs)
	}
	if got := changes["name"]; got.Old != "old" || got.New != "new" {
		t.Fatalf("name diff = %+v", got)
	}
	if got := changes["state"]; got.Old != "new" || got.New != "active" {
		t.Fatalf("state diff = %+v", got)
	}
	if _, ok := changes["version"]; ok {
		t.Fatal("managed version must not appear in the diff")
	}
	if _, ok := changes["updated_at"]; ok {
		t.Fatal("managed updated_at must not appear in the diff")
	}
}

func TestUpdateWithoutChangesSkipsWriteAndAudit(t *testing.T) {
	app, repo := newAuditedApplication(t, Options[*auditDoc, int64]{})
	ctx := operatorContext(t, "alice")
	created := mustCreateDoc(t, app, ctx, "same")

	loaded, err := app.Get(ctx, created.GetID())
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}
	if err := app.Update(ctx, loaded); err != nil {
		t.Fatalf("Update(): %v", err)
	}
	stored, err := app.Get(ctx, created.GetID())
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}
	if stored.GetVersion() != 0 {
		t.Fatalf("no-op update advanced version to %d", stored.GetVersion())
	}
	if records := auditRecords(t, repo, ctx, created.GetID()); len(records) != 1 {
		t.Fatalf("no-op update wrote audit records: %+v", records)
	}
}

func TestCreateRollsBackWhenAfterHookFails(t *testing.T) {
	want := errors.New("after create failed")
	app, repo := newAuditedApplication(t, Options[*auditDoc, int64]{
		Options: appcrud.Options[*auditDoc, int64]{
			Hooks: appcrud.Hooks[*auditDoc]{
				appcrud.ActionCreate: appcrud.Stage[*auditDoc]{
					After: func(context.Context, *auditDoc) error { return want },
				},
			},
		},
	})
	ctx := operatorContext(t, "alice")
	entity := &auditDoc{Name: "doomed"}

	if err := app.Create(ctx, entity); !errors.Is(err, want) {
		t.Fatalf("Create() = %v, want hook error", err)
	}
	if _, err := app.Get(ctx, entity.GetID()); !errors.Is(err, errors.NotFound) {
		t.Fatalf("entity survived rollback: %v", err)
	}
	if records := auditRecords(t, repo, ctx, entity.GetID()); len(records) != 0 {
		t.Fatalf("audit survived rollback: %+v", records)
	}
}

func TestCreateRollsBackWhenAuditStoreFails(t *testing.T) {
	repo := newAuditRepo()
	store := &failingAuditStore{
		AuditedMemoryRepository: repo,
		err:                     errors.NewCode(errors.Internal, "audit store down"),
	}
	app, _ := newAuditedApplicationWith(t, repo, store, Options[*auditDoc, int64]{})
	ctx := operatorContext(t, "alice")

	err := app.Create(ctx, &auditDoc{Name: "a"})
	requireErrorCode(t, err, errors.Internal)
	count, err := app.Count(ctx)
	if err != nil || count != 0 {
		t.Fatalf("entity survived audit failure, Count() = %d, %v", count, err)
	}
}

func TestCreateRejectsUnmarshalableSnapshot(t *testing.T) {
	app, _ := newAuditedApplication(t, Options[*auditDoc, int64]{})
	ctx := operatorContext(t, "alice")
	entity := &auditDoc{Name: "a", Extra: failingAuditValue("unserializable")}

	err := app.Create(ctx, entity)
	requireErrorCode(t, err, errors.Internal)
	count, err := app.Count(ctx)
	if err != nil || count != 0 {
		t.Fatalf("entity survived snapshot failure, Count() = %d, %v", count, err)
	}
}

type failingAuditValue string

func (failingAuditValue) MarshalJSON() ([]byte, error) {
	return nil, errors.NewCode(errors.Internal, "audit serialization failed")
}
