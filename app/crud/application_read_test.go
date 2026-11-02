package crud

import (
	"context"
	"gochen/app/internal/execution"
	"gochen/app/query"
	"gochen/auth"
	"gochen/domain/meta"
	"testing"

	domaincrud "gochen/domain/crud"
	"gochen/domain/spec"
	"gochen/errors"
	"gochen/testkit"
)

func TestListNormalizesPageBounds(t *testing.T) {
	app, _ := newTestApplication(t, Options[*testEntity, int64]{
		Limits: execution.Limits{MaxPageSize: 2},
	})
	ctx := context.Background()
	mustCreate(t, app, ctx, "a")
	mustCreate(t, app, ctx, "b")
	mustCreate(t, app, ctx, "c")

	first, err := app.List(ctx, -5, 0)
	if err != nil {
		t.Fatalf("List(): %v", err)
	}
	if len(first) != 2 || first[0].Name != "a" || first[1].Name != "b" {
		t.Fatalf("first page = %+v", first)
	}

	second, err := app.List(ctx, 1, 10)
	if err != nil {
		t.Fatalf("List(): %v", err)
	}
	if len(second) != 2 || second[0].Name != "b" || second[1].Name != "c" {
		t.Fatalf("second page = %+v", second)
	}

	empty, err := app.List(ctx, 3, 2)
	if err != nil {
		t.Fatalf("List(): %v", err)
	}
	if empty == nil || len(empty) != 0 {
		t.Fatalf("out of range page = %+v", empty)
	}
}

func TestGetExistsCountAndQuery(t *testing.T) {
	app, _ := newTestApplication(t, Options[*testEntity, int64]{})
	ctx := context.Background()
	a := mustCreate(t, app, ctx, "a")
	mustCreate(t, app, ctx, "b")

	got, err := app.Get(ctx, a.GetID())
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}
	if got.Name != "a" {
		t.Fatalf("Get() = %+v", got)
	}

	exists, err := app.Exists(ctx, a.GetID())
	if err != nil || !exists {
		t.Fatalf("Exists() = %v, %v", exists, err)
	}
	missing, err := app.Exists(ctx, 99)
	if err != nil || missing {
		t.Fatalf("Exists(missing) = %v, %v", missing, err)
	}

	count, err := app.Count(ctx)
	if err != nil || count != 2 {
		t.Fatalf("Count() = %d, %v", count, err)
	}

	items, err := app.Query(ctx, query.QueryOptions{Filters: eqFilter("name", "b")})
	if err != nil {
		t.Fatalf("Query(): %v", err)
	}
	if len(items) != 1 || items[0].Name != "b" {
		t.Fatalf("Query() = %+v", items)
	}

	sorted, err := app.Query(ctx, query.QueryOptions{
		Sorts: []query.Sort{{Field: "name", Direction: query.DESC}},
	})
	if err != nil {
		t.Fatalf("Query(sort): %v", err)
	}
	if len(sorted) != 2 || sorted[0].Name != "b" || sorted[1].Name != "a" {
		t.Fatalf("sorted = %+v", sorted)
	}

	if _, err := app.Query(ctx, query.QueryOptions{Filters: eqFilter("unknown", "x")}); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("Query(unknown field) error = %v, want InvalidInput", err)
	}
}

func TestQueryNormalizesPageBounds(t *testing.T) {
	app, _ := newTestApplication(t, Options[*testEntity, int64]{
		Limits: execution.Limits{MaxPageSize: 2},
	})
	ctx := context.Background()
	for _, name := range []string{"a", "b", "c"} {
		mustCreate(t, app, ctx, name)
	}
	items, err := app.Query(ctx, query.QueryOptions{Offset: -1, Limit: 100})
	if err != nil {
		t.Fatalf("Query(): %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected page size cap 2, got %d", len(items))
	}
}

func TestQueryCountUsesFiltersAndOneAuthorization(t *testing.T) {
	repo := newTestRepo()
	for i, owner := range []string{"alice", "alice", "bob"} {
		if err := repo.Create(t.Context(), &testEntity{Entity: domaincrud.Entity[int64]{ID: int64(i + 1)}, Owner: owner, Name: "match"}, spec.All()); err != nil {
			t.Fatal(err)
		}
	}
	authorizer := newCountingAuthorizer()
	authorizer.inner = fixedAuthorizer{decision: auth.Decision{Allowed: true, Spec: spec.Eq(meta.ByDimension("owner"), "alice")}, vocabulary: auth.Vocabulary{Refs: []meta.Ref{meta.ByDimension("owner")}}}
	app, _ := newTestApplicationWithRepo(t, repo, Options[*testEntity, int64]{Authorizer: authorizer})
	count, err := app.QueryCount(t.Context(), query.QueryOptions{Offset: 100, Limit: 1, Fields: []string{"unknown"}, Sorts: []query.Sort{{Field: "unknown"}}, Filters: eqFilter("name", "match")})
	if err != nil || count != 2 {
		t.Fatalf("filtered count = %d, %v", count, err)
	}
	if authorizer.calls[ActionList] != 1 {
		t.Fatalf("authorization calls = %v", authorizer.calls)
	}
	count, err = app.QueryCount(t.Context(), query.QueryOptions{Filters: eqFilter("name", "absent")})
	if err != nil || count != 0 {
		t.Fatalf("empty count = %d, %v", count, err)
	}
}

func TestApplicationPropagatesAuthorizerSpec(t *testing.T) {
	repo, repositoryErr := testkit.NewMemoryRepository[*testEntity, int64](testkit.NewInt64Sequence(100))
	if repositoryErr != nil {
		t.Fatal(repositoryErr)
	}
	ownerSpec := fixedAuthorizer{
		decision:   auth.Decision{Allowed: true, Spec: spec.Eq(meta.ByDimension("owner"), "alice")},
		vocabulary: auth.Vocabulary{Refs: []meta.Ref{meta.ByDimension("owner")}},
	}
	app, _ := newTestApplicationWithRepo(t, repo, Options[*testEntity, int64]{
		Authorizer: ownerSpec,
	})
	ctx := context.Background()
	if err := repo.Create(ctx, &testEntity{
		Entity: domaincrud.Entity[int64]{ID: 1},
		Owner:  "alice",
		Name:   "mine",
	}, spec.All()); err != nil {
		t.Fatalf("seed alice: %v", err)
	}
	if err := repo.Create(ctx, &testEntity{
		Entity: domaincrud.Entity[int64]{ID: 2},
		Owner:  "bob",
		Name:   "other",
	}, spec.All()); err != nil {
		t.Fatalf("seed bob: %v", err)
	}

	if _, err := app.Get(ctx, 1); err != nil {
		t.Fatalf("Get(visible): %v", err)
	}
	if _, err := app.Get(ctx, 2); !errors.Is(err, errors.NotFound) {
		t.Fatalf("Get(filtered) error = %v, want NotFound", err)
	}
	items, err := app.List(ctx, 0, 10)
	if err != nil {
		t.Fatalf("List(): %v", err)
	}
	if len(items) != 1 || items[0].GetID() != 1 {
		t.Fatalf("List() = %+v", items)
	}
	count, err := app.Count(ctx)
	if err != nil || count != 1 {
		t.Fatalf("Count() = %d, %v", count, err)
	}

	if err := app.Create(ctx, &testEntity{Owner: "bob", Name: "intruder"}); !errors.Is(err, errors.Forbidden) {
		t.Fatalf("Create(outside spec) error = %v, want Forbidden", err)
	}
	created := &testEntity{Owner: "alice", Name: "ok"}
	if err := app.Create(ctx, created); err != nil {
		t.Fatalf("Create(inside spec): %v", err)
	}
	if created.GetID() == 0 {
		t.Fatal("expected generated ID")
	}
}

func TestCreateAppliesServerAssignments(t *testing.T) {
	repo := newTestRepo()
	assigning := fixedAuthorizer{
		decision: auth.Decision{
			Allowed: true,
			Spec:    spec.All(),
			Assign:  []auth.Assignment{{Dimension: "owner", Value: "mike"}},
		},
		vocabulary: auth.Vocabulary{Refs: []meta.Ref{meta.ByDimension("owner")}, Assignable: []meta.Dimension{"owner"}},
	}
	app, _ := newTestApplicationWithRepo(t, repo, Options[*testEntity, int64]{Authorizer: assigning})
	ctx := context.Background()

	entity := &testEntity{Name: "assigned"}
	if err := app.Create(ctx, entity); err != nil {
		t.Fatalf("Create(): %v", err)
	}
	stored, err := app.Get(ctx, entity.GetID())
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}
	if stored.Owner != "mike" {
		t.Fatalf("owner = %q, want mike", stored.Owner)
	}

	conflict := &testEntity{Owner: "bob", Name: "conflict"}
	if err := app.Create(ctx, conflict); !errors.Is(err, errors.Forbidden) {
		t.Fatalf("Create(conflicting assignment) error = %v, want Forbidden", err)
	}
}

func TestNewApplicationRejectsUnknownNamedCondition(t *testing.T) {
	authorizer := fixedAuthorizer{
		decision:   auth.Decision{Allowed: true, Spec: spec.Named("active")},
		vocabulary: auth.Vocabulary{},
	}
	repo := newTestRepo()
	app, err := NewApplication[*testEntity, int64](repo, Options[*testEntity, int64]{
		Resource:   "doc",
		Authorizer: authorizer,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = app.Get(context.Background(), 1)
	if !errors.Is(err, errors.Internal) {
		t.Fatalf("Get() error = %v, want Internal", err)
	}
}

func TestReadRunsCustomActionWithSpec(t *testing.T) {
	app, _ := newTestApplication(t, Options[*testEntity, int64]{
		Actions: []auth.Action{{Name: "export", Kind: auth.KindList}},
	})
	ctx := context.Background()
	mustCreate(t, app, ctx, "a")

	var captured spec.Spec
	err := app.Read(ctx, "export", func(_ context.Context, s spec.Spec) error {
		captured = s
		return nil
	})
	if err != nil {
		t.Fatalf("Read(): %v", err)
	}
	if err := captured.Err(); err != nil {
		t.Fatalf("captured spec is invalid: %v", err)
	}
	matched, err := spec.NewInterpreter().Match(captured, &testEntity{})
	if err != nil || !matched {
		t.Fatalf("captured spec did not pass through allow authorizer: %v/%v", matched, err)
	}

	if err := app.Read(ctx, "unknown", func(context.Context, spec.Spec) error { return nil }); !errors.Is(err, errors.Forbidden) {
		t.Fatalf("Read(unknown) error = %v, want Forbidden", err)
	}
	if err := app.Read(ctx, "export", nil); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("Read(nil) error = %v, want InvalidInput", err)
	}
}

func TestReadApplicationServesListAndQuery(t *testing.T) {
	repo := newTestRepo()
	ctx := context.Background()
	if err := repo.Create(ctx, &testEntity{
		Entity: domaincrud.Entity[int64]{ID: 1},
		Name:   "a",
	}, spec.All()); err != nil {
		t.Fatalf("seed: %v", err)
	}
	app, err := NewReadApplication[*testEntity, int64](repo, Options[*testEntity, int64]{
		Resource:   "doc",
		Authorizer: auth.Allow(),
	})
	if err != nil {
		t.Fatalf("new read application: %v", err)
	}
	items, err := app.List(ctx, 0, 10)
	if err != nil || len(items) != 1 {
		t.Fatalf("List() = %+v, %v", items, err)
	}
	count, err := app.Count(ctx)
	if err != nil || count != 1 {
		t.Fatalf("Count() = %d, %v", count, err)
	}
	queried, err := app.Query(ctx, query.QueryOptions{Filters: eqFilter("name", "a")})
	if err != nil || len(queried) != 1 {
		t.Fatalf("Query() = %+v, %v", queried, err)
	}
}

func TestAuditTrailRequiresAuditStore(t *testing.T) {
	app, _ := newTestApplication(t, Options[*testEntity, int64]{})
	if _, err := app.AuditTrail(context.Background(), 1, 0, 10); !errors.Is(err, errors.Unsupported) {
		t.Fatalf("AuditTrail() error = %v, want Unsupported", err)
	}
}

func TestApplicationWithAuditStoreWritesTrail(t *testing.T) {
	repo, repositoryErr := testkit.NewAuditedMemoryRepository[*auditEntity, int64](testkit.NewInt64Sequence(1))
	if repositoryErr != nil {
		t.Fatal(repositoryErr)
	}
	app, err := NewApplication[*auditEntity, int64](repo, Options[*auditEntity, int64]{
		Resource:   "doc",
		Authorizer: auth.Allow(),
		Audit:      repo,
	})
	if err != nil {
		t.Fatalf("new audited crud application: %v", err)
	}
	ctx := newOperatorContext(t)
	entity := &auditEntity{Name: "first"}
	if err := app.Create(ctx, entity); err != nil {
		t.Fatalf("Create(): %v", err)
	}
	records, err := app.AuditTrail(ctx, entity.GetID(), 0, 10)
	if err != nil {
		t.Fatalf("AuditTrail(): %v", err)
	}
	if len(records) != 1 || records[0].Action != ActionCreate || records[0].Operator != "alice" {
		t.Fatalf("audit records = %+v", records)
	}

	if err := app.Create(context.Background(), &auditEntity{Name: "no operator"}); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("Create(without operator) error = %v, want InvalidInput", err)
	}
}
