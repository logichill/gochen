package audited

import (
	"context"
	"testing"

	appcrud "gochen/app/crud"
	"gochen/app/internal/execution"
	"gochen/auth"
	"gochen/contextx"
	"gochen/domain/spec"
	"gochen/errors"
)

type countingAuthorizer struct {
	calls int
}

func (a *countingAuthorizer) Authorize(context.Context, auth.Request) (auth.Decision, error) {
	a.calls++
	return auth.Decision{Allowed: true, Spec: spec.All()}, nil
}

func (a *countingAuthorizer) Vocabulary() auth.Vocabulary { return auth.Vocabulary{} }

func TestDeleteBulkSoftDeletesAndWritesSingleBulkAudit(t *testing.T) {
	repo := newAuditRepo()
	store := &recordingAuditStore{AuditedMemoryRepository: repo}
	app, _ := newAuditedApplicationWith(t, repo, store, Options[*auditDoc, int64]{})
	ctx := operatorContext(t, "alice")
	first := mustCreateDoc(t, app, ctx, "a")
	second := mustCreateDoc(t, app, ctx, "b")
	third := mustCreateDoc(t, app, ctx, "c")

	store.singleCalls = 0
	store.bulkCalls = 0
	store.lastBulk = 0

	deleteCtx := operatorContext(t, "bob")
	if err := app.DeleteBulk(deleteCtx, []int64{first.GetID(), third.GetID()}); err != nil {
		t.Fatalf("DeleteBulk(): %v", err)
	}
	if store.bulkCalls != 1 || store.singleCalls != 0 || store.lastBulk != 2 {
		t.Fatalf("audit store calls = bulk:%d single:%d last:%d", store.bulkCalls, store.singleCalls, store.lastBulk)
	}

	visible, err := app.List(ctx, 0, 10)
	if err != nil || len(visible) != 1 || visible[0].GetID() != second.GetID() {
		t.Fatalf("List() = %+v, %v", visible, err)
	}
	deleted, err := app.ListDeleted(ctx, 0, 10)
	if err != nil || len(deleted) != 2 {
		t.Fatalf("ListDeleted() = %d, %v", len(deleted), err)
	}
	for _, id := range []int64{first.GetID(), third.GetID()} {
		records := auditRecords(t, repo, ctx, id)
		if len(records) != 2 || records[1].Action != actionDelete || records[1].Operator != "bob" {
			t.Fatalf("entity %d audit records = %+v", id, records)
		}
	}
}

func TestDeleteBulkMissingIDRollsBackWholeBulk(t *testing.T) {
	app, repo := newAuditedApplication(t, Options[*auditDoc, int64]{})
	ctx := operatorContext(t, "alice")
	first := mustCreateDoc(t, app, ctx, "a")
	second := mustCreateDoc(t, app, ctx, "b")

	err := app.DeleteBulk(ctx, []int64{first.GetID(), 999})
	if !errors.Is(err, errors.NotFound) {
		t.Fatalf("DeleteBulk(missing id) = %v, want NotFound", err)
	}
	visible, err := app.List(ctx, 0, 10)
	if err != nil || len(visible) != 2 {
		t.Fatalf("List() = %d, %v", len(visible), err)
	}
	for _, id := range []int64{first.GetID(), second.GetID()} {
		if records := auditRecords(t, repo, ctx, id); len(records) != 1 {
			t.Fatalf("entity %d audit records = %d, want 1", id, len(records))
		}
	}
}

func TestDeleteBulkRejectsDuplicateIDs(t *testing.T) {
	app, repo := newAuditedApplication(t, Options[*auditDoc, int64]{})
	ctx := operatorContext(t, "alice")
	doc := mustCreateDoc(t, app, ctx, "a")

	err := app.DeleteBulk(ctx, []int64{doc.GetID(), doc.GetID()})
	if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("DeleteBulk(duplicate ids) = %v, want InvalidInput", err)
	}
	if _, err := app.Get(ctx, doc.GetID()); err != nil {
		t.Fatalf("entity deleted by rejected bulk: %v", err)
	}
	if records := auditRecords(t, repo, ctx, doc.GetID()); len(records) != 1 {
		t.Fatalf("audit records = %d, want 1", len(records))
	}
}

func TestDeleteBulkRejectsOversizedBulk(t *testing.T) {
	options := Options[*auditDoc, int64]{
		Options: appcrud.Options[*auditDoc, int64]{
			Limits: execution.Limits{MaxBulkSize: 1},
		},
	}
	app, _ := newAuditedApplication(t, options)
	ctx := operatorContext(t, "alice")
	first := mustCreateDoc(t, app, ctx, "a")
	second := mustCreateDoc(t, app, ctx, "b")

	err := app.DeleteBulk(ctx, []int64{first.GetID(), second.GetID()})
	if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("DeleteBulk(oversized) = %v, want InvalidInput", err)
	}
}

func TestDeleteBulkEmptyBulkSkipsAuthorization(t *testing.T) {
	authorizer := &countingAuthorizer{}
	app, _ := newAuditedApplication(t, Options[*auditDoc, int64]{
		Options: appcrud.Options[*auditDoc, int64]{Authorizer: authorizer},
	})
	if err := app.DeleteBulk(context.Background(), nil); err != nil {
		t.Fatalf("DeleteBulk(nil) = %v", err)
	}
	if authorizer.calls != 0 {
		t.Fatalf("authorization calls = %d, want 0", authorizer.calls)
	}
}

func TestDeleteBulkRequiresOperator(t *testing.T) {
	app, _ := newAuditedApplication(t, Options[*auditDoc, int64]{})
	ctx := operatorContext(t, "alice")
	doc := mustCreateDoc(t, app, ctx, "a")

	err := app.DeleteBulk(context.Background(), []int64{doc.GetID()})
	if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("DeleteBulk() without operator = %v, want InvalidInput", err)
	}
	if _, err := app.Get(ctx, doc.GetID()); err != nil {
		t.Fatalf("entity deleted despite rejected bulk: %v", err)
	}
}

func TestExecuteNewBulkWritesSingleBulkAudit(t *testing.T) {
	repo := newAuditRepo()
	store := &recordingAuditStore{AuditedMemoryRepository: repo}
	app, _ := newAuditedApplicationWith(t, repo, store, Options[*auditDoc, int64]{
		Options: appcrud.Options[*auditDoc, int64]{
			Actions: []auth.Action{{Name: "import", Kind: auth.KindCreate}},
		},
	})
	ctx := operatorContext(t, "alice")

	results, err := app.ExecuteNewBulk(ctx, "import", func(context.Context) ([]*auditDoc, error) {
		return []*auditDoc{{Name: "a", State: "new"}, {Name: "b", State: "new"}}, nil
	})
	if err != nil {
		t.Fatalf("ExecuteNewBulk(): %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	if store.bulkCalls != 1 || store.singleCalls != 0 || store.lastBulk != 2 {
		t.Fatalf("audit store calls = bulk:%d single:%d last:%d", store.bulkCalls, store.singleCalls, store.lastBulk)
	}
	for _, result := range results {
		records := auditRecords(t, repo, ctx, result.GetID())
		if len(records) != 1 || records[0].Action != "import" {
			t.Fatalf("entity %d audit records = %+v", result.GetID(), records)
		}
	}
}

func TestExecuteNewBulkPostCommitFailureKeepsCommittedWrites(t *testing.T) {
	postCommitCalls := 0
	app, repo := newAuditedApplication(t, Options[*auditDoc, int64]{
		Options: appcrud.Options[*auditDoc, int64]{
			Actions: []auth.Action{{Name: "import", Kind: auth.KindCreate}},
			Hooks: appcrud.Hooks[*auditDoc]{
				"import": {
					PostCommit: func(context.Context, *auditDoc) error {
						postCommitCalls++
						return errors.NewCode(errors.Internal, "notification failed")
					},
				},
			},
		},
	})
	ctx := operatorContext(t, "alice")

	_, err := app.ExecuteNewBulk(ctx, "import", func(context.Context) ([]*auditDoc, error) {
		return []*auditDoc{{Name: "a"}, {Name: "b"}}, nil
	})
	if !contextx.IsAfterCommitError(err) {
		t.Fatalf("ExecuteNewBulk() = %v, want after-commit error", err)
	}
	if postCommitCalls != 2 {
		t.Fatalf("post-commit calls = %d, want 2", postCommitCalls)
	}
	count, err := app.Count(ctx)
	if err != nil || count != 2 {
		t.Fatalf("Count() = %d, %v", count, err)
	}
	for _, id := range []int64{1, 2} {
		if records := auditRecords(t, repo, ctx, id); len(records) != 1 {
			t.Fatalf("entity %d audit records = %d, want 1", id, len(records))
		}
	}
}
