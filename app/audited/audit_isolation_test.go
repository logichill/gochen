package audited

import (
	"context"
	"testing"

	"gochen/contextx"
	domaudited "gochen/domain/audited"
)

func TestAuditTrailIsolatesResourceAndTenantAfterPurge(t *testing.T) {
	app, repo := newAuditedApplication(t, Options[*auditDoc, int64]{})
	for _, record := range []domaudited.AuditRecord{
		{ResourceKind: "doc", TenantID: "t1", EntityID: "42", Action: "purge", Operator: "alice"},
		{ResourceKind: "invoice", TenantID: "t1", EntityID: "42", Action: "update", Operator: "alice"},
		{ResourceKind: "doc", TenantID: "t2", EntityID: "42", Action: "update", Operator: "bob"},
		{ResourceKind: "doc", EntityID: "42", Action: "legacy", Operator: "alice"},
	} {
		if _, err := repo.SaveAuditRecord(context.Background(), record); err != nil {
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
		if err != nil {
			t.Fatal(err)
		}
		if len(records) != 1 || records[0].ResourceKind != "doc" || records[0].TenantID != tenant {
			t.Fatalf("tenant %q: unexpected records %+v", tenant, records)
		}
	}
}
