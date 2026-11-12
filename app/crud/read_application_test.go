package crud

import (
	"context"
	"reflect"
	"testing"

	"gochen/auth"
	"gochen/domain/meta"
	"gochen/domain/spec"
	"gochen/errors"
)

func TestReadApplicationDoesNotExposeWriteMethods(t *testing.T) {
	application, err := NewReadApplication(newTestRepo(), Options[*testEntity, int64]{Resource: "doc", Authorizer: auth.Allow()})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Create", "Update", "Delete", "DeleteBulk", "Execute", "ExecuteNew", "ExecuteBulk", "ExecuteNewBulk", "Transfer", "MaxBulkSize"} {
		if _, exists := reflect.TypeOf(application).MethodByName(name); exists {
			t.Errorf("read application exposes %s", name)
		}
	}
}

func TestReadApplicationPreservesAuthorizationAndCustomRead(t *testing.T) {
	repository := newTestRepo()
	for i, owner := range []string{"alice", "bob"} {
		if err := repository.Create(t.Context(), &testEntity{Entity: domaincrudEntity(int64(i + 1)), Owner: owner}, spec.All()); err != nil {
			t.Fatal(err)
		}
	}
	authorizer := newCountingAuthorizer()
	authorizer.inner = fixedAuthorizer{
		decision:   auth.Decision{Allowed: true, Spec: spec.Eq(meta.ByDimension("owner"), "alice")},
		vocabulary: auth.Vocabulary{Refs: []meta.Ref{meta.ByDimension("owner")}},
	}
	application, err := NewReadApplication(repository, Options[*testEntity, int64]{
		Resource: "doc", Authorizer: authorizer,
		Actions: []auth.Action{{Name: "export", Kind: auth.KindList}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Get(t.Context(), 2); !errors.Is(err, errors.NotFound) {
		t.Fatalf("Get outside scope = %v", err)
	}
	if count, err := application.Count(t.Context()); err != nil || count != 1 {
		t.Fatalf("Count = %d, %v", count, err)
	}
	if application.UsesAllowAuthorizer() {
		t.Fatal("scoped application reports an allow-all authorizer")
	}
	if err := application.Read(t.Context(), "export", func(ctx context.Context, scope spec.Spec) error {
		count, err := repository.Count(ctx, scope)
		if err != nil || count != 1 {
			t.Fatalf("custom read count = %d, %v", count, err)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{ActionRead, ActionList, "export"} {
		if authorizer.calls[action] != 1 {
			t.Errorf("%s authorization calls = %d", action, authorizer.calls[action])
		}
	}
}
