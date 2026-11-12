package deletion

import (
	"context"
	"testing"

	domaudited "gochen/audit"
	"gochen/contextx"
	"gochen/domain/meta"
	"gochen/errors"
	"gochen/testkit"
)

func TestAuditTrailIsolatesResourceAndTenantAfterPurge(t *testing.T) {
	repo, err := testkit.NewAuditedMemoryRepository[*auditDoc, int64](testkit.NewInt64Sequence(1), testkit.WithMemoryIsolationField[*auditDoc, int64](meta.ByDimension("tenant"), nil))
	if err != nil {
		t.Fatal(err)
	}
	app, repo := newAuditedApplicationWith(t, repo, repo, testOptions[*auditDoc, int64]{})
	for _, record := range []domaudited.AuditRecord{
		{ResourceKind: "doc", EntityID: "42", Action: "purge", Operator: "alice", OperationID: "test-operation", Dimensions: auditSnapshot(t,
			"t1",
			0, ""),
		},
		{ResourceKind: "invoice", EntityID: "42", Action: "update", Operator: "alice", OperationID: "test-operation", Dimensions: auditSnapshot(t,
			"t1",
			0, ""),
		},
		{ResourceKind: "doc", EntityID: "42", Action: "update", Operator: "bob", OperationID: "test-operation", Dimensions: auditSnapshot(t,
			"t2",
			0, ""),
		},
		{ResourceKind: "doc", EntityID: "42", Dimensions: auditSnapshot(t, "", 0, ""), Action: "legacy", Operator: "alice", OperationID: "test-operation"},
	} {
		tenant := auditTenant(record)
		ctx := context.Background()
		if tenant == "" {
			if _, err := repo.SaveAuditRecord(ctx, record); !errors.Is(err, errors.Forbidden) {
				t.Fatalf("legacy snapshot: %v", err)
			}
			continue
		}
		ctx, err = contextx.WithTenantID(ctx, tenant)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := repo.SaveAuditRecord(ctx, record); err != nil {
			t.Fatal(err)
		}
	}
	for _, tenant := range []string{"t1", "t2", ""} {
		ctx := context.Background()
		if tenant != "" {
			var err error
			ctx, err = contextx.WithTenantID(ctx, tenant)
			if err != nil {
				t.Fatal(err)
			}
		}
		records, err := app.AuditTrail(ctx, 42, 0, 10)
		if tenant == "" {
			if !errors.Is(err, errors.Forbidden) {
				t.Fatalf("missing isolation: %v", err)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if len(records) != 1 || records[0].ResourceKind != "doc" || auditTenant(records[0]) != tenant {
			t.Fatalf("tenant %q: unexpected records %+v", tenant, records)
		}
	}
}
