package crud

import (
	"context"

	"strings"
	"testing"

	"gochen/errors"
)

func TestApplicationValidateLayersDomainBeforeApplicationPolicy(t *testing.T) {
	order := make([]string, 0, 2)
	app := &Application[*selfValidatingEntity, int64]{
		config:    DefaultServiceConfig(),
		validator: orderedValidator{order: &order},
	}
	if err := app.Validate(&selfValidatingEntity{id: 1, order: &order}); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if got := strings.Join(order, ","); got != "domain,application" {
		t.Fatalf("validation order = %q", got)
	}
}

func TestApplicationValidateStopsAfterDomainInvariantFailure(t *testing.T) {
	order := make([]string, 0, 2)
	domainErr := errors.New("domain invariant failed")
	app := &Application[*selfValidatingEntity, int64]{
		config:    DefaultServiceConfig(),
		validator: orderedValidator{order: &order},
	}
	err := app.Validate(&selfValidatingEntity{id: 1, order: &order, validate: domainErr})
	if !errors.Is(err, domainErr) {
		t.Fatalf("Validate() error = %v", err)
	}
	if got := strings.Join(order, ","); got != "domain" {
		t.Fatalf("validation order = %q", got)
	}
}

func TestApplication_NewApplicationNormalizesZeroServiceConfig(t *testing.T) {
	repo := newBatchTestRepo()
	app, err := NewApplication(repo, &Options[testEntity, int64]{Config: &ServiceConfig{}})
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	cfg := app.Config()
	if cfg.MaxBatchSize != defaultMaxBatchSize {
		t.Fatalf("expected default max batch size, got %d", cfg.MaxBatchSize)
	}
	if cfg.MaxPageSize != defaultMaxPageSize {
		t.Fatalf("expected default max page size, got %d", cfg.MaxPageSize)
	}

	err = app.CreateAll(context.Background(), []testEntity{{id: 1}})
	if err != nil {
		t.Fatalf("expected zero config batch size to be normalized, got %v", err)
	}
}

func TestApplication_NormalizesNegativeServiceConfig(t *testing.T) {
	repo := newBatchTestRepo()
	app, err := NewApplication(repo, &Options[testEntity, int64]{Config: &ServiceConfig{
		MaxBatchSize: -1,
		MaxPageSize:  -1,
	}})
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	cfg := app.Config()
	if cfg.MaxBatchSize != defaultMaxBatchSize {
		t.Fatalf("expected default max batch size, got %d", cfg.MaxBatchSize)
	}
	if cfg.MaxPageSize != defaultMaxPageSize {
		t.Fatalf("expected default max page size, got %d", cfg.MaxPageSize)
	}

}

func TestApplication_ConfigReturnsCopy(t *testing.T) {
	repo := newBatchTestRepo()
	app, err := NewApplication(repo, &Options[testEntity, int64]{Config: DefaultServiceConfig()})
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	cfg := app.Config()
	cfg.MaxBatchSize = 1

	got := app.Config()
	if got.MaxBatchSize == 1 {
		t.Fatalf("expected Config to return copy, got shared pointer")
	}
}

type narrowCRUDRepo struct{}

func (narrowCRUDRepo) Create(context.Context, testEntity) error       { return nil }
func (narrowCRUDRepo) Get(context.Context, int64) (testEntity, error) { return testEntity{}, nil }
func (narrowCRUDRepo) Update(context.Context, testEntity) error       { return nil }
func (narrowCRUDRepo) Delete(context.Context, int64) error            { return nil }

func TestApplication_CapabilityProbes(t *testing.T) {
	narrowApp, err := NewApplication[testEntity, int64](narrowCRUDRepo{}, nil)
	if err != nil {
		t.Fatalf("new application: %v", err)
	}
	if narrowApp.HasQueryCapability() {
		t.Fatalf("expected narrowCRUDRepo to lack query capability")
	}
	if narrowApp.HasQueryableCapability() {
		t.Fatalf("expected narrowCRUDRepo to lack queryable capability")
	}
	if narrowApp.HasBatchCapability() {
		t.Fatalf("expected narrowCRUDRepo to lack batch capability")
	}

	queryOnlyApp, err := NewApplication[testEntity, int64](&recordingRepo{}, nil)
	if err != nil {
		t.Fatalf("new application: %v", err)
	}
	if !queryOnlyApp.HasQueryCapability() {
		t.Fatalf("expected recordingRepo to have query capability")
	}
	if queryOnlyApp.HasQueryableCapability() {
		t.Fatalf("expected recordingRepo to lack queryable capability")
	}
	if queryOnlyApp.HasBatchCapability() {
		t.Fatalf("expected recordingRepo to lack batch capability")
	}

	queryableApp, err := NewApplication[testEntity, int64](&queryRepo{}, nil)
	if err != nil {
		t.Fatalf("new application: %v", err)
	}
	if !queryableApp.HasQueryCapability() {
		t.Fatalf("expected queryRepo to have query capability")
	}
	if !queryableApp.HasQueryableCapability() {
		t.Fatalf("expected queryRepo to have queryable capability")
	}

	txBatchApp, err := NewApplication[testEntity, int64](newBatchTestRepo(), nil)
	if err != nil {
		t.Fatalf("new application: %v", err)
	}
	if !txBatchApp.HasBatchCapability() {
		t.Fatalf("expected transactional repo to have batch capability")
	}
}
