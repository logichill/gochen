package audited_test

import (
	"context"
	"encoding/json"
	appaudited "gochen/app/audited"
	"gochen/auth"
	"gochen/contextx"
	"gochen/domain/audited"
	"gochen/domain/meta"
	"gochen/domain/spec"
	"gochen/errors"
	"gochen/testkit"
	"gochen/testkit/contracttest"
	"testing"
)

type rejectingTransferAudit struct {
	repo   *testkit.AuditedMemoryRepository[*contracttest.Document, int64]
	second bool
}

func (s *rejectingTransferAudit) DataSourceID() string { return s.repo.DataSourceID() }
func (s *rejectingTransferAudit) ListAuditRecords(ctx context.Context, id string, offset, limit int, condition spec.Spec) ([]audited.AuditRecord, error) {
	return s.repo.ListAuditRecords(ctx, id, offset, limit, condition)
}
func (s *rejectingTransferAudit) SaveAuditRecord(ctx context.Context, record audited.AuditRecord) (int64, error) {
	if s.second && record.Action == "transfer_in" {
		return 0, errors.NewCode(errors.Internal, "second audit failed")
	}
	return s.repo.SaveAuditRecord(ctx, record)
}

func TestTransferAuditFailureRollsBackBothWritesAndProjectionPreservesFacts(t *testing.T) {
	repo, err := testkit.NewAuditedMemoryRepository[*contracttest.Document, int64](testkit.NewInt64Sequence(1), testkit.WithMemoryResourceContract[*contracttest.Document, int64](contracttest.Contract()), testkit.WithMemoryIsolationField[*contracttest.Document, int64](meta.ByDimension("tenant"), nil))
	if err != nil {
		t.Fatal(err)
	}
	store := &rejectingTransferAudit{repo: repo, second: true}
	application, err := appaudited.NewApplication(repo, store, appaudited.Options[*contracttest.Document, int64]{
		Resource: "document", Authorizer: &transferPolicy{},
		Actions: []auth.Action{{Name: "move", Kind: auth.KindTransfer, ReceiveAction: "receive", Dimensions: []meta.Dimension{"department", "region"}}, {Name: "receive", Kind: auth.KindTransfer}},
		Projector: func(entity *contracttest.Document) (any, error) {
			department := entity.Department
			// Projector 使用独立展示副本，模拟脱敏时意外改写输入。
			entity.Department = 999
			return map[string]any{"department": department}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := contextx.WithTenantID(context.Background(), "t1")
	if err != nil {
		t.Fatal(err)
	}
	ctx, err = contextx.WithOperator(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	entity := &contracttest.Document{Department: 1, Name: "original"}
	// 初始事实通过仓储创建，测试专注迁移投影。
	if err := repo.Create(ctx, entity, spec.All()); err != nil {
		t.Fatal(err)
	}
	targets := []auth.Assignment{{Dimension: "department", Value: 2}}
	if _, err := application.Transfer(ctx, "move", entity.ID, entity.Version, targets); !errors.Is(err, errors.Internal) {
		t.Fatalf("audit failure=%v", err)
	}
	stored, err := repo.Get(ctx, entity.ID, spec.All())
	if err != nil {
		t.Fatal(err)
	}
	if stored.Department != 1 || stored.Version != entity.Version {
		t.Fatal("audit failure committed entity")
	}
	readCtx := audited.WithAuditResourceKind(ctx, "document")
	records, err := repo.ListAuditRecords(readCtx, "1", 0, 10, spec.All())
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Fatal("audit failure retained first record")
	}
	store.second = false
	after, err := application.Transfer(ctx, "move", entity.ID, entity.Version, targets)
	if err != nil {
		t.Fatal(err)
	}
	if after.Department != 2 {
		t.Fatal("projector changed actual after image")
	}
	records, err = repo.ListAuditRecords(readCtx, "1", 0, 10, spec.All())
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("records=%d", len(records))
	}
	for _, record := range records {
		value, _ := record.Dimensions.Value("department")
		if value != int64(1) && value != int64(2) {
			t.Fatalf("projected fact=%v", value)
		}
		var changes []audited.FieldChange
		if err := json.Unmarshal(record.Changes, &changes); err != nil {
			t.Fatal(err)
		}
		if len(changes) != 1 || changes[0].Field != "department" || changes[0].Old != float64(1) || changes[0].New != float64(2) {
			t.Fatalf("changes=%v", changes)
		}
	}
}
