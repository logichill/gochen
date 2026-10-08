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

	writer := NewBatchWriter(app)
	err = writer.CreateAll(context.Background(), []testEntity{{id: 1}})
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
