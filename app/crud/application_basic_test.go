package crud

import (
	"context"
	"strings"
	"testing"

	"gochen/app"
	"gochen/auth"
	"gochen/domain/spec"
	"gochen/errors"
	"gochen/testkit"
)

func TestNewApplicationRejectsInvalidOptions(t *testing.T) {
	repo := newTestRepo()
	cases := []struct {
		name    string
		options Options[*testEntity, int64]
	}{
		{
			name:    "missing authorizer",
			options: Options[*testEntity, int64]{DeleteMode: DeletePurge, Resource: "doc"},
		},
		{
			name:    "empty resource",
			options: Options[*testEntity, int64]{DeleteMode: DeletePurge, Authorizer: auth.Allow()},
		},
		{
			name: "duplicate action",
			options: Options[*testEntity, int64]{DeleteMode: DeletePurge,
				Resource:   "doc",
				Authorizer: auth.Allow(),
				Actions:    []auth.Action{{Name: ActionRead, Kind: auth.KindRead}},
			},
		},
		{
			name: "invalid action kind",
			options: Options[*testEntity, int64]{DeleteMode: DeletePurge,
				Resource:   "doc",
				Authorizer: auth.Allow(),
				Actions:    []auth.Action{{Name: "weird", Kind: auth.Kind("bogus")}},
			},
		},
		{
			name: "audit_read without audit store",
			options: Options[*testEntity, int64]{DeleteMode: DeletePurge,
				Resource:   "doc",
				Authorizer: auth.Allow(),
				Actions:    []auth.Action{{Name: "audit_read", Kind: auth.KindRead}},
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewApplication[*testEntity, int64](repo, tt.options); !errors.Is(err, errors.InvalidInput) {
				t.Fatalf("NewApplication() error = %v, want InvalidInput", err)
			}
		})
	}
}

func TestNewApplicationRejectsNilRepository(t *testing.T) {
	if _, err := NewApplication[*testEntity, int64](nil, Options[*testEntity, int64]{DeleteMode: DeletePurge,
		Resource:   "doc",
		Authorizer: auth.Allow(),
	}); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("NewApplication(nil) error = %v, want InvalidInput", err)
	}
	var repo *testkit.MemoryRepository[*testEntity, int64]
	if _, err := NewApplication[*testEntity, int64](repo, Options[*testEntity, int64]{DeleteMode: DeletePurge,
		Resource:   "doc",
		Authorizer: auth.Allow(),
	}); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("NewApplication(typed nil) error = %v, want InvalidInput", err)
	}
}

func TestNewApplicationBuildsStandardDescriptor(t *testing.T) {
	app, _ := newTestApplication(t, Options[*testEntity, int64]{DeleteMode: DeletePurge})
	descriptor := app.Descriptor()
	if descriptor.Resource != "doc" {
		t.Fatalf("resource = %q", descriptor.Resource)
	}
	names := make([]string, 0, len(descriptor.Actions))
	for _, action := range descriptor.Actions {
		names = append(names, action.Name)
	}
	if got := strings.Join(names, ","); got != "read,list,create,update,purge" {
		t.Fatalf("actions = %q", got)
	}
	if _, ok := descriptor.Action("audit_read"); ok {
		t.Fatal("audit_read must not be declared without an audit store")
	}
	permission, ok := descriptor.PermissionFor(ActionRead)
	if !ok || permission != "doc:api:read" {
		t.Fatalf("permission = %q, %v", permission, ok)
	}
	if app.MaxPageSize() != 1000 || app.MaxBulkSize() != 1000 {
		t.Fatalf("default limits = %d/%d", app.MaxBulkSize(), app.MaxPageSize())
	}
}

func TestUsesAllowAuthorizerReflectsPolicy(t *testing.T) {
	openApp, _ := newTestApplication(t, Options[*testEntity, int64]{DeleteMode: DeletePurge})
	if !openApp.UsesAllowAuthorizer() {
		t.Fatal("auth.Allow application must report UsesAllowAuthorizer")
	}

	repo := newTestRepo()
	restricted, err := NewApplication[*testEntity, int64](repo, Options[*testEntity, int64]{DeleteMode: DeletePurge,
		Resource:   "doc",
		Authorizer: fixedAuthorizer{decision: auth.Decision{Allowed: true}},
	})
	if err != nil {
		t.Fatalf("new application: %v", err)
	}
	if restricted.UsesAllowAuthorizer() {
		t.Fatal("custom authorizer must not report UsesAllowAuthorizer")
	}
}

func TestNewReadApplicationOnlyDeclaresReadActions(t *testing.T) {
	repo := newTestRepo()
	app, err := NewReadApplication[*testEntity, int64](repo, Options[*testEntity, int64]{
		Resource:   "doc",
		Authorizer: auth.Allow(),
	})
	if err != nil {
		t.Fatalf("new read application: %v", err)
	}
	descriptor := app.Descriptor()
	if len(descriptor.Actions) != 2 {
		t.Fatalf("actions = %+v", descriptor.Actions)
	}
	for _, action := range descriptor.Actions {
		if action.Name != ActionRead && action.Name != ActionList {
			t.Fatalf("unexpected action %q", action.Name)
		}
	}

	if _, ok := any(app).(IWriteApplication[*testEntity, int64]); ok {
		t.Fatal("read application exposes standard writes")
	}
	if _, ok := any(app).(IActionExecutor[*testEntity, int64]); ok {
		t.Fatal("read application exposes write action execution")
	}
	if _, ok := any(app).(ITransferApplication[*testEntity, int64]); ok {
		t.Fatal("read application exposes transfer")
	}

	if _, err := NewReadApplication[*testEntity, int64](nil, Options[*testEntity, int64]{
		Resource:   "doc",
		Authorizer: auth.Allow(),
	}); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("NewReadApplication(nil) error = %v, want InvalidInput", err)
	}

	if _, err := NewReadApplication[*testEntity, int64](repo, Options[*testEntity, int64]{
		Resource:   "doc",
		Authorizer: auth.Allow(),
		Actions:    []auth.Action{{Name: "import", Kind: auth.KindCreate}},
	}); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("read application with write action error = %v, want InvalidInput", err)
	}
}

func TestCreateValidatesDomainBeforeApplicationPolicy(t *testing.T) {
	order := make([]string, 0, 2)
	repo, repositoryErr := testkit.NewMemoryRepository[*validatingEntity, int64](testkit.NewInt64Sequence(1))
	if repositoryErr != nil {
		t.Fatal(repositoryErr)
	}
	app, err := NewApplication[*validatingEntity, int64](repo, Options[*validatingEntity, int64]{DeleteMode: DeletePurge,
		Resource:   "doc",
		Authorizer: auth.Allow(),
		Validator:  orderedValidator{order: &order},
	})
	if err != nil {
		t.Fatalf("new application: %v", err)
	}
	entity := &validatingEntity{Name: "ok", Order: &order}
	if err := app.Create(context.Background(), entity); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if got := strings.Join(order, ","); got != "domain,application" {
		t.Fatalf("validation order = %q", got)
	}
	if entity.GetID() == 0 {
		t.Fatal("expected repository to assign an ID")
	}
}

func TestCreateStopsAfterDomainInvariantFailure(t *testing.T) {
	order := make([]string, 0, 2)
	repo, repositoryErr := testkit.NewMemoryRepository[*validatingEntity, int64](testkit.NewInt64Sequence(1))
	if repositoryErr != nil {
		t.Fatal(repositoryErr)
	}
	app, err := NewApplication[*validatingEntity, int64](repo, Options[*validatingEntity, int64]{DeleteMode: DeletePurge,
		Resource:   "doc",
		Authorizer: auth.Allow(),
		Validator:  orderedValidator{order: &order},
	})
	if err != nil {
		t.Fatalf("new application: %v", err)
	}
	domainErr := errors.New("domain invariant failed")
	entity := &validatingEntity{Name: "bad", Order: &order, domainErr: domainErr}
	if err := app.Create(context.Background(), entity); !errors.Is(err, domainErr) {
		t.Fatalf("Create() error = %v, want domain error", err)
	}
	if got := strings.Join(order, ","); got != "domain" {
		t.Fatalf("validation order = %q", got)
	}
	count, err := repo.Count(context.Background(), spec.All())
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no persisted entity, got %d", count)
	}
}

func TestApplicationValidatorFailureLeavesNothingPersisted(t *testing.T) {
	validator := &trackingValidator{err: errors.New("policy rejected")}
	app, repo := newTestApplication(t, Options[*testEntity, int64]{DeleteMode: DeletePurge, Validator: validator})
	err := app.Create(context.Background(), &testEntity{Name: "x"})
	if !errors.Is(err, validator.err) {
		t.Fatalf("Create() error = %v", err)
	}
	if validator.called != 1 {
		t.Fatalf("validator calls = %d", validator.called)
	}
	count, countErr := repo.Count(context.Background(), spec.All())
	if countErr != nil {
		t.Fatalf("count: %v", countErr)
	}
	if count != 0 {
		t.Fatalf("expected no persisted entity, got %d", count)
	}
}

// 不完整的审计实现不能再进入公开构造参数；组合端口在编译期要求全部能力。
func TestAuditWriterDoesNotRequireQueryPort(t *testing.T) {
	if _, ok := any(saveOnlyAuditStore{}).(app.IAuditRepository); !ok {
		t.Fatal("write-only store must implement the transactional audit writer")
	}
}
