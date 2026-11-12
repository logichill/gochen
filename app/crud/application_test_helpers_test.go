package crud

import (
	"context"
	domcrud "gochen/domain/crud"
	"testing"

	"gochen/app"
	"gochen/app/query"
	domaudited "gochen/audit"
	"gochen/auth"
	"gochen/contextx"

	"gochen/domain/meta"
	"gochen/errors"
	"gochen/testkit"
	"gochen/validate"
)

type testEntity struct {
	domcrud.Entity[int64]
	Owner  string `json:"owner" dim:"owner"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

type validatingEntity struct {
	domcrud.Entity[int64]
	Name      string `json:"name"`
	Order     *[]string
	domainErr error
}

func (e *validatingEntity) Validate() error {
	*e.Order = append(*e.Order, "domain")
	return e.domainErr
}

type auditEntity struct {
	domcrud.TrackedEntity[int64]
	Name string `json:"name"`
}

type orderedValidator struct {
	order *[]string
	err   error
}

func (v orderedValidator) Validate(any) error {
	*v.order = append(*v.order, "application")
	return v.err
}

type trackingValidator struct {
	err    error
	called int
}

func (v *trackingValidator) Validate(any) error {
	v.called++
	return v.err
}

var _ validate.IValidator = (*trackingValidator)(nil)

type fixedAuthorizer struct {
	decision   auth.Decision
	vocabulary auth.Vocabulary
}

func (a fixedAuthorizer) Authorize(context.Context, auth.Request) (auth.Decision, error) {
	return a.decision, nil
}

func (a fixedAuthorizer) Vocabulary() auth.Vocabulary { return a.vocabulary }

type countingAuthorizer struct {
	inner auth.IAuthorizer
	calls map[string]int
}

func newCountingAuthorizer() *countingAuthorizer {
	return &countingAuthorizer{inner: auth.Allow(), calls: make(map[string]int)}
}

func (a *countingAuthorizer) Authorize(ctx context.Context, req auth.Request) (auth.Decision, error) {
	a.calls[req.Action]++
	return a.inner.Authorize(ctx, req)
}

func (a *countingAuthorizer) Vocabulary() auth.Vocabulary { return a.inner.Vocabulary() }

// saveOnlyAuditStore 只看写入能力，用于验证构造期要求 IAuditQueryStore。
type saveOnlyAuditStore struct{}

func (saveOnlyAuditStore) SaveAuditRecord(context.Context, domaudited.AuditRecord) (int64, error) {
	return 0, nil
}

func (saveOnlyAuditStore) DataSourceID() string { return testkit.MemoryDataSourceID }

func domaincrudEntity(id int64) domcrud.Entity[int64] {
	return domcrud.Entity[int64]{ID: id}
}

func newTestRepo() *testkit.MemoryRepository[*testEntity, int64] {
	repository, repositoryErr := testkit.NewMemoryRepository[*testEntity, int64](testkit.NewInt64Sequence(1), testkit.WithMemoryResourceContract[*testEntity, int64](app.ResourceContract{Assignable: []meta.Dimension{"owner"}}))
	if repositoryErr != nil {
		panic(repositoryErr)
	}
	return repository
}

func newTestApplication(t *testing.T, options Options[*testEntity, int64]) (*Application[*testEntity, int64], *testkit.MemoryRepository[*testEntity, int64]) {
	t.Helper()
	repo := newTestRepo()
	return newTestApplicationWithRepo(t, repo, options)
}

func newTestApplicationWithRepo(
	t *testing.T,
	repo *testkit.MemoryRepository[*testEntity, int64],
	options Options[*testEntity, int64],
) (*Application[*testEntity, int64], *testkit.MemoryRepository[*testEntity, int64]) {
	t.Helper()
	if options.Resource == "" {
		options.Resource = "doc"
	}
	if options.Authorizer == nil {
		options.Authorizer = auth.Allow()
	}
	app, err := NewApplication[*testEntity, int64](repo, options)
	if err != nil {
		t.Fatalf("new application: %v", err)
	}
	return app, repo
}

func newOperatorContext(t *testing.T) context.Context {
	t.Helper()
	ctx, err := contextx.WithOperator(context.Background(), "alice")
	if err != nil {
		t.Fatalf("with operator: %v", err)
	}
	return ctx
}

func eqFilter(field string, value string) query.QueryFilters {
	var filters query.QueryFilters
	return filters.Append(field, query.QueryExpr{
		Op:    query.FilterOpEq,
		Value: query.QueryValue{Type: query.FieldTypeString, Normalized: value, String: value},
	})
}

func mustCreate(t *testing.T, app *Application[*testEntity, int64], ctx context.Context, name string) *testEntity {
	t.Helper()
	entity := &testEntity{Name: name}
	if err := app.Create(ctx, entity); err != nil {
		t.Fatalf("create %q: %v", name, err)
	}
	return entity
}

func requireErrorCode(t *testing.T, err error, code errors.ErrorCode) {
	t.Helper()
	if !errors.Is(err, code) {
		t.Fatalf("error = %v, want code %v", err, code)
	}
}
