package deletion

import (
	"context"
	"encoding/json"
	"testing"

	"gochen/app"
	appcrud "gochen/app/crud"
	domaudited "gochen/audit"
	"gochen/auth"
	"gochen/domain/spec"
	"gochen/errors"
)

func updateActionOptions(action string) testOptions[*auditDoc, int64] {
	return testOptions[*auditDoc, int64]{
		Options: appcrud.Options[*auditDoc, int64]{
			Actions: []auth.Action{{Name: action, Kind: auth.KindUpdate}},
		},
	}
}

func TestExecuteBulkNoOpDoesNotWriteOrAudit(t *testing.T) {
	app, repo := newAuditedApplication(t, updateActionOptions("rename"))
	ctx := operatorContext(t, "alice")
	first := mustCreateDoc(t, app, ctx, "a")
	second := mustCreateDoc(t, app, ctx, "b")

	results, err := app.ExecuteBulk(ctx, "rename", []int64{first.GetID(), second.GetID()},
		func(context.Context, *auditDoc) error { return nil })
	if err != nil {
		t.Fatalf("ExecuteBulk(): %v", err)
	}
	if len(results) != 2 || results[0].GetID() != first.GetID() || results[1].GetID() != second.GetID() {
		t.Fatalf("results = %+v", results)
	}
	for _, id := range []int64{first.GetID(), second.GetID()} {
		if records := auditRecords(t, repo, ctx, id); len(records) != 1 {
			t.Fatalf("entity %d audit records = %d, want only create", id, len(records))
		}
		stored, err := app.Get(ctx, id)
		if err != nil {
			t.Fatalf("Get(%d): %v", id, err)
		}
		if stored.GetVersion() != 0 {
			t.Fatalf("no-op bulk advanced entity %d to version %d", id, stored.GetVersion())
		}
	}
}

func TestExecuteBulkAuditsOnlyChangedEntities(t *testing.T) {
	app, repo := newAuditedApplication(t, updateActionOptions("rename"))
	ctx := operatorContext(t, "alice")
	first := mustCreateDoc(t, app, ctx, "a")
	second := mustCreateDoc(t, app, ctx, "b")

	_, err := app.ExecuteBulk(ctx, "rename", []int64{first.GetID(), second.GetID()},
		func(_ context.Context, e *auditDoc) error {
			if e.Name == "b" {
				e.Name = "b2"
			}
			return nil
		})
	if err != nil {
		t.Fatalf("ExecuteBulk(): %v", err)
	}

	if records := auditRecords(t, repo, ctx, first.GetID()); len(records) != 1 {
		t.Fatalf("unchanged entity audit records = %d, want 1", len(records))
	}
	records := auditRecords(t, repo, ctx, second.GetID())
	if len(records) != 2 {
		t.Fatalf("changed entity audit records = %d, want 2", len(records))
	}
	if records[1].Action != "rename" {
		t.Fatalf("action = %q, want rename", records[1].Action)
	}
	var diffs []domaudited.FieldChange
	if err := json.Unmarshal(records[1].Changes, &diffs); err != nil {
		t.Fatalf("decode diff: %v", err)
	}
	if len(diffs) != 1 || diffs[0].Field != "name" || diffs[0].Old != "b" || diffs[0].New != "b2" {
		t.Fatalf("diffs = %+v", diffs)
	}
	stored, err := app.Get(ctx, second.GetID())
	if err != nil || stored.Name != "b2" || stored.GetVersion() != 1 {
		t.Fatalf("stored = %+v, %v", stored, err)
	}
}

func TestExecuteBulkDuplicateIDsRejected(t *testing.T) {
	app, repo := newAuditedApplication(t, updateActionOptions("rename"))
	ctx := operatorContext(t, "alice")
	doc := mustCreateDoc(t, app, ctx, "a")

	_, err := app.ExecuteBulk(ctx, "rename", []int64{doc.GetID(), doc.GetID()},
		func(context.Context, *auditDoc) error { return nil })
	if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("ExecuteBulk(duplicate ids) = %v, want InvalidInput", err)
	}
	if records := auditRecords(t, repo, ctx, doc.GetID()); len(records) != 1 {
		t.Fatalf("audit records = %d, want 1", len(records))
	}
}

func TestExecuteBulkExceedsBulkLimit(t *testing.T) {
	options := updateActionOptions("rename")
	options.Limits = app.Limits{MaxBulkSize: 1}
	app, _ := newAuditedApplication(t, options)
	ctx := operatorContext(t, "alice")
	first := mustCreateDoc(t, app, ctx, "a")
	second := mustCreateDoc(t, app, ctx, "b")

	_, err := app.ExecuteBulk(ctx, "rename", []int64{first.GetID(), second.GetID()},
		func(context.Context, *auditDoc) error { return nil })
	if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("ExecuteBulk(oversized) = %v, want InvalidInput", err)
	}
}

func TestExecuteBulkRollsBackOnCommandError(t *testing.T) {
	app, repo := newAuditedApplication(t, updateActionOptions("rename"))
	ctx := operatorContext(t, "alice")
	first := mustCreateDoc(t, app, ctx, "a")
	second := mustCreateDoc(t, app, ctx, "b")

	want := errors.NewCode(errors.Internal, "command failed")
	_, err := app.ExecuteBulk(ctx, "rename", []int64{first.GetID(), second.GetID()},
		func(_ context.Context, e *auditDoc) error {
			e.Name = e.Name + "-changed"
			if e.Name == "b-changed" {
				return want
			}
			return nil
		})
	if !errors.Is(err, want) {
		t.Fatalf("ExecuteBulk() = %v, want command error", err)
	}
	for _, expected := range []struct {
		id   int64
		name string
	}{{first.GetID(), "a"}, {second.GetID(), "b"}} {
		stored, err := app.Get(ctx, expected.id)
		if err != nil {
			t.Fatalf("Get(%d): %v", expected.id, err)
		}
		if stored.Name != expected.name || stored.GetVersion() != 0 {
			t.Fatalf("entity %d = %q/v%d, want %q/v0", expected.id, stored.Name, stored.GetVersion(), expected.name)
		}
		if records := auditRecords(t, repo, ctx, expected.id); len(records) != 1 {
			t.Fatalf("entity %d audit records = %d, want 1", expected.id, len(records))
		}
	}
}

func TestExecuteBulkMissingIDRollsBackWholeBulk(t *testing.T) {
	app, repo := newAuditedApplication(t, updateActionOptions("rename"))
	ctx := operatorContext(t, "alice")
	doc := mustCreateDoc(t, app, ctx, "a")

	_, err := app.ExecuteBulk(ctx, "rename", []int64{doc.GetID(), 999},
		func(_ context.Context, e *auditDoc) error {
			e.Name = "changed"
			return nil
		})
	if !errors.Is(err, errors.NotFound) {
		t.Fatalf("ExecuteBulk(missing id) = %v, want NotFound", err)
	}
	stored, err := app.Get(ctx, doc.GetID())
	if err != nil || stored.Name != "a" || stored.GetVersion() != 0 {
		t.Fatalf("stored = %+v, %v", stored, err)
	}
	if records := auditRecords(t, repo, ctx, doc.GetID()); len(records) != 1 {
		t.Fatalf("audit records = %d, want 1", len(records))
	}
}

func TestExecuteNewBulkRejectsOversizedBulk(t *testing.T) {
	app, repo := newAuditedApplication(t, testOptions[*auditDoc, int64]{
		Options: appcrud.Options[*auditDoc, int64]{
			Actions: []auth.Action{{Name: "import", Kind: auth.KindCreate}},
			Limits:  app.Limits{MaxBulkSize: 1},
		},
	})
	ctx := operatorContext(t, "alice")

	_, err := app.ExecuteNewBulk(ctx, "import", func(context.Context) ([]*auditDoc, error) {
		return []*auditDoc{{Name: "a"}, {Name: "b"}}, nil
	})
	if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("ExecuteNewBulk(oversized) = %v, want InvalidInput", err)
	}
	count, err := app.Count(ctx)
	if err != nil || count != 0 {
		t.Fatalf("Count() = %d, %v", count, err)
	}
	if records := auditRecords(t, repo, ctx, 1); len(records) != 0 {
		t.Fatalf("audit records = %d, want 0", len(records))
	}
}

func TestExecuteNewBulkRollsBackOnAfterHookFailure(t *testing.T) {
	want := errors.NewCode(errors.Internal, "after failed")
	app, repo := newAuditedApplication(t, testOptions[*auditDoc, int64]{
		Options: appcrud.Options[*auditDoc, int64]{
			Actions: []auth.Action{{Name: "import", Kind: auth.KindCreate}},
			Hooks: appcrud.Hooks[*auditDoc]{
				"import": {
					After: func(_ context.Context, e *auditDoc) error {
						if e.Name == "bad" {
							return want
						}
						return nil
					},
				},
			},
		},
	})
	ctx := operatorContext(t, "alice")

	_, err := app.ExecuteNewBulk(ctx, "import", func(context.Context) ([]*auditDoc, error) {
		return []*auditDoc{{Name: "good"}, {Name: "bad"}}, nil
	})
	if !errors.Is(err, want) {
		t.Fatalf("ExecuteNewBulk() = %v, want hook error", err)
	}
	count, err := app.Count(ctx)
	if err != nil || count != 0 {
		t.Fatalf("Count() = %d, %v", count, err)
	}
	for _, id := range []int64{1, 2} {
		if records := auditRecords(t, repo, ctx, id); len(records) != 0 {
			t.Fatalf("entity %d audit records = %d, want 0", id, len(records))
		}
	}
}

func TestExecuteBulkPostCommitRunsAfterCommit(t *testing.T) {
	postCommitCalls := 0
	sawCommitted := true
	repo := newAuditRepo()
	app, _ := newAuditedApplicationWith(t, repo, repo, testOptions[*auditDoc, int64]{
		Options: appcrud.Options[*auditDoc, int64]{
			Actions: []auth.Action{{Name: "rename", Kind: auth.KindUpdate}},
			Hooks: appcrud.Hooks[*auditDoc]{
				"rename": {
					PostCommit: func(_ context.Context, e *auditDoc) error {
						postCommitCalls++
						stored, err := repo.Get(context.Background(), e.GetID(), spec.All())
						if err != nil || stored.Name != e.Name {
							sawCommitted = false
						}
						return nil
					},
				},
			},
		},
	})
	ctx := operatorContext(t, "alice")
	first := mustCreateDoc(t, app, ctx, "a")
	second := mustCreateDoc(t, app, ctx, "b")

	_, err := app.ExecuteBulk(ctx, "rename", []int64{first.GetID(), second.GetID()},
		func(_ context.Context, e *auditDoc) error {
			e.Name = e.Name + "2"
			return nil
		})
	if err != nil {
		t.Fatalf("ExecuteBulk(): %v", err)
	}
	if postCommitCalls != 2 {
		t.Fatalf("post-commit calls = %d, want 2", postCommitCalls)
	}
	if !sawCommitted {
		t.Fatal("post-commit hook did not observe committed state")
	}
}
