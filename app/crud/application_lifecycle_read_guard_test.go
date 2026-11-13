package crud

import (
	"context"
	"testing"

	"gochen/app/query"
	"gochen/auth"
	"gochen/domain/spec"
	"gochen/errors"
)

func TestGenericReadRejectsLifecycleActionsBeforeAuthorization(t *testing.T) {
	repo := newDeletionRepo(t)
	policy := &deletionRequests{allow: ActionListDeleted}
	app, err := NewApplication(repo, Options[*auditEntity, int64]{Resource: "document", Authorizer: policy, DeleteMode: DeletePurge, EnableDeletedQuery: true, EnableRestore: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{ActionDelete, ActionRestore, ActionPurge, ActionListDeleted, "audit_read", " delete ", " restore ", " purge ", " list_deleted ", " audit_read "} {
		t.Run(action, func(t *testing.T) {
			called := false
			err := app.Read(t.Context(), action, func(context.Context, spec.Spec) error { called = true; return nil })
			if !errors.Is(err, errors.Forbidden) || called || len(policy.requests) != 0 {
				t.Fatalf("generic read borrowed lifecycle action: err=%v callback=%t requests=%+v", err, called, policy.requests)
			}
		})
	}
}

func TestDeletedListPermissionCannotAuthorizeOrdinaryReadEntrypoints(t *testing.T) {
	repo := newDeletionRepo(t)
	policy := &deletionRequests{allow: ActionListDeleted}
	app, err := NewApplication(repo, Options[*auditEntity, int64]{Resource: "document", Authorizer: policy, DeleteMode: DeleteSoft, EnableDeletedQuery: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range []struct {
		name, action string
		run          func() error
	}{
		{"Get", ActionRead, func() error { _, err := app.Get(t.Context(), 1); return err }},
		{"Exists", ActionRead, func() error { _, err := app.Exists(t.Context(), 1); return err }},
		{"List", ActionList, func() error { _, err := app.List(t.Context(), 0, 10); return err }},
		{"Query", ActionList, func() error { _, err := app.Query(t.Context(), query.QueryOptions{}); return err }},
		{"QueryCount", ActionList, func() error { _, err := app.QueryCount(t.Context(), query.QueryOptions{}); return err }},
		{"Count", ActionList, func() error { _, err := app.Count(t.Context()); return err }},
		{"Read(list)", ActionList, func() error {
			return app.Read(t.Context(), ActionList, func(context.Context, spec.Spec) error { t.Fatal("unauthorized read callback"); return nil })
		}},
	} {
		t.Run(entry.name, func(t *testing.T) {
			policy.requests = nil
			if err := entry.run(); !errors.Is(err, errors.Forbidden) {
				t.Fatalf("ordinary entry accepted deleted-list-only permission: %v", err)
			}
			if len(policy.requests) != 1 || policy.requests[0].Action != entry.action {
				t.Fatalf("ordinary entry used wrong action: %+v", policy.requests)
			}
		})
	}
	policy.requests = nil
	if _, err := app.ListDeleted(t.Context(), 0, 10); err != nil {
		t.Fatalf("dedicated deleted-list rejected: %v", err)
	}
	if len(policy.requests) != 1 || policy.requests[0].Action != ActionListDeleted {
		t.Fatalf("dedicated action changed: %+v", policy.requests)
	}
}

func TestGenericReadKeepsNormalAndCustomListActions(t *testing.T) {
	repo := newDeletionRepo(t)
	policy := &deletionRequests{}
	app, err := NewApplication(repo, Options[*auditEntity, int64]{Resource: "document", Authorizer: policy, DeleteMode: DeletePurge, Actions: []auth.Action{{Name: "export", Kind: auth.KindList}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{ActionList, "export"} {
		called := false
		if err := app.Read(t.Context(), action, func(context.Context, spec.Spec) error { called = true; return nil }); err != nil || !called {
			t.Fatalf("legitimate list action changed: %v called=%t", err, called)
		}
	}
	if len(policy.requests) != 2 || policy.requests[0].Action != ActionList || policy.requests[1].Action != "export" {
		t.Fatalf("wrong normal/custom actions: %+v", policy.requests)
	}
}
