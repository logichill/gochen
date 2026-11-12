package testkit_test

import (
	"context"
	"encoding/json"
	domcrud "gochen/domain/crud"
	"testing"

	audited "gochen/audit"
	"gochen/contextx"
	"gochen/domain/meta"
	"gochen/domain/spec"
	"gochen/errors"
	"gochen/testkit"
	"gochen/testkit/contracttest"
)

type auditDocument struct {
	domcrud.TrackedEntity[int64]
	TenantID   string
	Department int64 `dim:"department"`
}

func TestMemoryAuditOrdinaryFieldIsolationAndTypedRoundTrip(t *testing.T) {
	repository, err := testkit.NewAuditedMemoryRepository[*auditDocument, int64](testkit.NewInt64Sequence(1),
		testkit.WithMemoryIsolationField[*auditDocument, int64](meta.ByField("TenantID"), nil))
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := contextx.WithTenantID(context.Background(), "t1")
	if err != nil {
		t.Fatal(err)
	}
	var snapshot audited.DimensionSnapshot
	if err := json.Unmarshal([]byte(`{"department":7}`), &snapshot); err != nil {
		t.Fatal(err)
	}
	record := audited.AuditRecord{ResourceKind: "document", EntityID: "1", Action: "update", Operator: "alice", OperationID: "op", Dimensions: snapshot}
	if _, err := repository.SaveAuditRecord(ctx, record); err != nil {
		t.Fatal(err)
	}
	records, err := repository.ListAuditRecords(ctx, "1", 0, 10, spec.Eq(meta.ByDimension("department"), "7"))
	if err != nil || len(records) != 1 {
		t.Fatalf("records=%v, err=%v", records, err)
	}
	value, _ := records[0].Dimensions.Value("department")
	if value != int64(7) {
		t.Fatalf("typed value=%T(%v)", value, value)
	}
	other, err := contextx.WithTenantID(ctx, "t2")
	if err != nil {
		t.Fatal(err)
	}
	records, err = repository.ListAuditRecords(other, "1", 0, 10, spec.All())
	if err != nil || len(records) != 0 {
		t.Fatalf("other records=%v, err=%v", records, err)
	}
	if _, err := repository.SaveAuditRecord(context.Background(), record); errors.Code(err) != errors.Forbidden {
		t.Fatalf("missing isolation=%v", err)
	}
}

func TestMemoryAuditRejectsIsolationMismatchAndDuplicatePairAtomically(t *testing.T) {
	repository, err := testkit.NewAuditedMemoryRepository[*contracttest.Document, int64](testkit.NewInt64Sequence(1),
		testkit.WithMemoryIsolationField[*contracttest.Document, int64](meta.ByDimension("tenant"), nil))
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := contextx.WithTenantID(context.Background(), "t1")
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := meta.Describe[*contracttest.Document]()
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := audited.CaptureDimensions(metadata, &contracttest.Document{Tenant: "t2", Department: 1})
	if err != nil {
		t.Fatal(err)
	}
	record := audited.AuditRecord{ResourceKind: "document", EntityID: "1", Action: "transfer_out", Operator: "alice", OperationID: "op", Dimensions: snapshot}
	if _, err := repository.SaveAuditRecord(ctx, record); errors.Code(err) != errors.Forbidden {
		t.Fatalf("isolation mismatch=%v", err)
	}
	snapshot, err = audited.CaptureDimensions(metadata, &contracttest.Document{Tenant: "t1", Department: 1})
	if err != nil {
		t.Fatal(err)
	}
	record.Dimensions = snapshot
	incoming := record
	incoming.Action = "transfer_in"
	if err := repository.SaveAuditRecords(ctx, []audited.AuditRecord{record, incoming}); err != nil {
		t.Fatal(err)
	}
	fresh := record
	fresh.OperationID = "fresh"
	freshIn := fresh
	freshIn.Action = "transfer_in"
	err = repository.SaveAuditRecords(ctx, []audited.AuditRecord{fresh, freshIn, record, incoming})
	if errors.Code(err) != errors.Conflict {
		t.Fatalf("duplicate=%v", err)
	}
	records, err := repository.ListAuditRecords(ctx, "1", 0, 10, spec.All())
	if err != nil || len(records) != 2 {
		t.Fatalf("atomic records=%v, err=%v", records, err)
	}
}
