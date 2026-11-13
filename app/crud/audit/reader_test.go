package audit

import (
	"context"
	"gochen/app"
	records "gochen/audit"
	"gochen/auth"
	"gochen/contextx"
	"gochen/domain/crud"
	"gochen/domain/meta"
	"gochen/domain/spec"
	"gochen/errors"
	"gochen/testkit"
	"testing"
)

type document struct {
	crud.Entity[int64]
	Tenant string `json:"tenant" dim:"tenant"`
	Owner  string `json:"owner" dim:"owner"`
	Name   string `json:"name"`
}
type emptyProvider struct{}

func (emptyProvider) ExecutionDescriptor() app.ExecutionDescriptor { return app.ExecutionDescriptor{} }

type checkedAuthorizer struct {
	request  *auth.Request
	vocab    auth.Vocabulary
	decision auth.Decision
}

func (a checkedAuthorizer) Vocabulary() auth.Vocabulary { return a.vocab }
func (a checkedAuthorizer) Authorize(_ context.Context, r auth.Request) (auth.Decision, error) {
	if a.request != nil {
		*a.request = r
	}
	return a.decision, nil
}

func TestReaderConstructorAndContextValidation(t *testing.T) {
	repo, err := testkit.NewAuditedMemoryRepository[*document, int64](testkit.NewInt64Sequence(1))
	if err != nil {
		t.Fatal(err)
	}
	valid := Options{Resource: "document", Authorizer: auth.Allow()}
	var nilStore *testkit.AuditedMemoryRepository[*document, int64]
	tests := []struct {
		name     string
		store    records.IAuditQueryStore
		provider app.IExecutionDescriptorProvider
		options  Options
	}{
		{"nil store", nilStore, repo, valid}, {"nil provider", repo, nil, valid}, {"missing metadata", repo, emptyProvider{}, valid},
		{"nil authorizer", repo, repo, Options{Resource: "document"}},
		{"bad vocabulary", repo, repo, Options{Resource: "document", Authorizer: checkedAuthorizer{vocab: auth.Vocabulary{Refs: []meta.Ref{meta.ByDimension("missing")}}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewReader[int64](tt.store, tt.provider, tt.options); err == nil {
				t.Fatal("invalid reader accepted")
			}
		})
	}
	reader, err := NewReader[int64](repo, repo, valid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.AuditTrail(nil, 1, 0, 1); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("nil context: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := reader.AuditTrail(ctx, 1, 0, 1); err != context.Canceled {
		t.Fatalf("canceled context: %v", err)
	}
}

func TestReaderAuthorizesHistoryAndFiltersSnapshotsWithoutResourceRow(t *testing.T) {
	repo, err := testkit.NewAuditedMemoryRepository[*document, int64](testkit.NewInt64Sequence(1), testkit.WithMemoryIsolationField[*document, int64](meta.ByDimension("tenant"), nil))
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := meta.Describe[*document]()
	if err != nil {
		t.Fatal(err)
	}
	for _, tenant := range []string{"t1", "t2"} {
		ctx, err := contextx.WithTenantID(context.Background(), tenant)
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err := records.CaptureDimensions(metadata, &document{Tenant: tenant, Owner: "alice"})
		if err != nil {
			t.Fatal(err)
		}
		for _, resource := range []string{"document", "invoice"} {
			if _, err := repo.SaveAuditRecord(ctx, records.AuditRecord{ResourceKind: resource, EntityID: "42", Action: "purge", Operator: "alice", OperationID: resource + tenant, Dimensions: snapshot}); err != nil {
				t.Fatal(err)
			}
		}
	}
	var requested auth.Request
	authorizer := checkedAuthorizer{request: &requested, vocab: auth.Vocabulary{Refs: []meta.Ref{meta.ByDimension("owner")}}, decision: auth.Decision{Allowed: true, Spec: spec.Eq(meta.ByDimension("owner"), "alice")}}
	reader, err := NewReader[int64](repo, repo, Options{Resource: "document", Authorizer: authorizer, Permission: "history:view"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := contextx.WithTenantID(context.Background(), "t1")
	if err != nil {
		t.Fatal(err)
	}
	history, err := reader.AuditTrail(ctx, 42, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].ResourceKind != "document" {
		t.Fatalf("unexpected history: %+v", history)
	}
	if requested.Action != ActionRead || requested.Permission != "history:view" || requested.Resource != "document" || len(requested.IDs) != 1 || requested.IDs[0] != "42" {
		t.Fatalf("wrong history authorization: %+v", requested)
	}
	value, _ := history[0].Dimensions.Value("tenant")
	if value != "t1" {
		t.Fatalf("tenant escaped: %v", value)
	}
	denied, err := NewReader[int64](repo, repo, Options{Resource: "document", Authorizer: checkedAuthorizer{decision: auth.Decision{Allowed: false}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := denied.AuditTrail(ctx, 42, 0, 10); !errors.Is(err, errors.Forbidden) {
		t.Fatalf("history bypassed denial: %v", err)
	}
}
