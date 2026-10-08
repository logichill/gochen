package audited

import (
	"context"
	"encoding/json"

	"strings"
	"testing"
	"time"

	"gochen/contextx"

	"gochen/domain/audited"
	"gochen/domain/crud"
	"gochen/errors"
)

// TestNewApplication_RequiresAuditStore 验证 NewApplication RequiresAuditStore。
func TestNewApplication_RequiresAuditStore(t *testing.T) {
	repo := newMemAuditedRepo()
	_, err := NewApplication(repo, nil, nil)
	if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected InvalidInput, got %v", err)
	}
}

// TestAuditedApplication_Create_RollsBackWhenAuditStoreFails 验证 AuditedApplication Create RollsBackWhenAuditStoreFails。
func TestAuditedApplication_Create_RollsBackWhenAuditStoreFails(t *testing.T) {
	repo := newMemAuditedRepo()
	store := failingAuditStore{err: errors.NewCode(errors.Internal, "audit store down")}
	app, err := NewApplication(repo, store, nil)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	ctx, err := contextx.WithOperator(context.Background(), "alice")
	if err != nil {
		t.Fatalf("WithOperator returned error: %v", err)
	}
	e := &auditedTestEntity{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1}}}
	if err := app.Create(ctx, e); err == nil {
		t.Fatalf("expected create to fail when audit store fails")
	}
	if _, err := repo.Get(context.Background(), 1); !errors.Is(err, errors.NotFound) {
		t.Fatalf("expected entity not persisted when audit store fails, got err=%v", err)
	}
}

// TestAuditedApplication_GetAuditTrail_UsesStore 验证 AuditedApplication AuditTrail UsesStore。
func TestAuditedApplication_GetAuditTrail_UsesStore(t *testing.T) {
	repo := newMemAuditedRepo()
	store := &recordingAuditStore{}
	app, err := NewApplication(repo, store, nil)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	ctx, err := contextx.WithOperator(context.Background(), "alice")
	if err != nil {
		t.Fatalf("WithOperator returned error: %v", err)
	}
	e := &auditedTestEntity{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1}}}
	if err := app.Create(ctx, e); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	records, err := app.AuditTrail(context.Background(), 1, 0, 100)
	if err != nil {
		t.Fatalf("get audit trail: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].EntityID != "1" {
		t.Fatalf("unexpected record: %+v", records[0])
	}

	// 缺失 store 的场景会在构造阶段被阻断；这里用编译期断言防止未来回退。
	if _, err := NewApplication(repo, nil, nil); err == nil {
		t.Fatalf("expected constructor to fail without auditStore")
	}
}

// TestAuditedApplication_Update_RecordsFieldDiff 验证 AuditedApplication Update RecordsFieldDiff。
func TestAuditedApplication_Update_RecordsFieldDiff(t *testing.T) {
	repo := newMemAuditedRepo()
	store := &recordingAuditStore{}
	app, err := NewApplication(repo, store, nil)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	// 预置实体
	e := &auditedTestEntity{
		AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1, Version: 1}},
		Name:          "old_name",
		Status:        "pending",
	}
	repo.items[1] = e

	// 更新实体
	ctx, err := contextx.WithOperator(context.Background(), "alice")
	if err != nil {
		t.Fatalf("WithOperator returned error: %v", err)
	}
	updated := &auditedTestEntity{
		AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1, Version: 1}},
		Name:          "new_name",
		Status:        "active",
	}
	if err := app.Update(ctx, updated); err != nil {
		t.Fatalf("update: %v", err)
	}

	// 验证审计记录
	if len(store.records) != 1 {
		t.Fatalf("expected 1 audit record, got %d", len(store.records))
	}
	rec := store.records[0]
	if rec.Operation != audited.AuditOpUpdate {
		t.Fatalf("expected UPDATE operation, got %s", rec.Operation)
	}
	if rec.Operator != "alice" {
		t.Fatalf("expected operator alice, got %s", rec.Operator)
	}

	// 验证 changes 包含 diff
	if rec.Changes == nil {
		t.Fatalf("expected changes to contain diff, got nil")
	}

	var diffs []audited.FieldChange
	if err := json.Unmarshal(rec.Changes, &diffs); err != nil {
		t.Fatalf("failed to unmarshal changes: %v", err)
	}

	// 检查是否记录了 name 和 status 的变更
	diffMap := make(map[string]audited.FieldChange)
	for _, d := range diffs {
		diffMap[d.Field] = d
	}

	if nameDiff, ok := diffMap["name"]; !ok {
		t.Fatalf("expected diff for 'name' field")
	} else {
		if nameDiff.Old != "old_name" || nameDiff.New != "new_name" {
			t.Fatalf("unexpected name diff: old=%v, new=%v", nameDiff.Old, nameDiff.New)
		}
	}

	if statusDiff, ok := diffMap["status"]; !ok {
		t.Fatalf("expected diff for 'status' field")
	} else {
		if statusDiff.Old != "pending" || statusDiff.New != "active" {
			t.Fatalf("unexpected status diff: old=%v, new=%v", statusDiff.Old, statusDiff.New)
		}
	}
}

func TestAuditedApplication_Update_RollsBackWhenDiffMarshalFails(t *testing.T) {
	repo := newMemAuditedRepo()
	store := &recordingAuditStore{}
	app, err := NewApplication(repo, store, nil)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	original := &auditedTestEntity{
		AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1, Version: 1}},
		Name:          "original",
	}
	repo.items[1] = cloneAuditedTestEntity(original)

	ctx, err := contextx.WithOperator(context.Background(), "alice")
	if err != nil {
		t.Fatalf("WithOperator returned error: %v", err)
	}
	updated := &auditedTestEntity{
		AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1, Version: 1}},
		Name:          "updated",
		Snapshot:      make(chan int),
	}

	if err := app.Update(ctx, updated); !errors.Is(err, errors.Internal) {
		t.Fatalf("expected Internal when diff marshal fails, got %v", err)
	}
	got, err := repo.Get(context.Background(), 1)
	if err != nil {
		t.Fatalf("get after failed update: %v", err)
	}
	if got.Name != original.Name {
		t.Fatalf("expected update rollback, got name=%s", got.Name)
	}
	if len(store.records) != 0 {
		t.Fatalf("expected no audit record after diff marshal failure, got %d", len(store.records))
	}
}

func TestAuditedApplication_Update_NoChangeKeepsNilDiff(t *testing.T) {
	repo := newMemAuditedRepo()
	store := &recordingAuditStore{}
	app, err := NewApplication(repo, store, nil)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	repo.items[1] = &auditedTestEntity{
		AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1, Version: 1}},
		Name:          "same",
		Status:        "same",
	}

	ctx, err := contextx.WithOperator(context.Background(), "alice")
	if err != nil {
		t.Fatalf("WithOperator returned error: %v", err)
	}
	updated := &auditedTestEntity{
		AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1, Version: 1}},
		Name:          "same",
		Status:        "same",
	}
	if err := app.Update(ctx, updated); err != nil {
		t.Fatalf("update: %v", err)
	}
	if len(store.records) != 1 {
		t.Fatalf("expected 1 audit record, got %d", len(store.records))
	}
	if store.records[0].Changes != nil {
		t.Fatalf("expected nil changes for no-change update, got %s", string(store.records[0].Changes))
	}
}

// TestComputeEntityDiff_ReturnsNilWhenNoChanges 验证 ComputeEntityDiff ReturnsNilWhenNoChanges。
func TestComputeEntityDiff_ReturnsNilWhenNoChanges(t *testing.T) {
	e := &auditedTestEntity{
		AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1, Version: 1}},
		Name:          "same",
		Status:        "same",
	}
	diff, err := computeEntityDiff(e, e)
	if err != nil {
		t.Fatalf("compute diff: %v", err)
	}
	if diff != nil {
		t.Fatalf("expected nil diff for identical entities, got %s", string(diff))
	}
}

func TestComputeEntityDiff_ReturnsDecodeError(t *testing.T) {
	diff, err := computeEntityDiff(invalidDiffJSONEntity{}, invalidDiffJSONEntity{})
	if !errors.Is(err, errors.Internal) {
		t.Fatalf("expected Internal decode error, got diff=%s err=%v", string(diff), err)
	}
	if diff != nil {
		t.Fatalf("expected nil diff when decode fails, got %s", string(diff))
	}
}

// TestComputeEntityDiff_SortedFieldOrder 验证 ComputeEntityDiff SortedFieldOrder。
func TestComputeEntityDiff_SortedFieldOrder(t *testing.T) {
	type testEntity struct {
		Name   string `json:"name"`
		Status string `json:"status"`
		ZField string `json:"z_field"`
		AField string `json:"a_field"`
	}

	before := &testEntity{Name: "a", Status: "b", ZField: "z", AField: "a"}
	after := &testEntity{Name: "x", Status: "y", ZField: "w", AField: "v"}

	// 多次运行确保顺序稳定
	for i := 0; i < 10; i++ {
		diff, err := computeEntityDiff(before, after)
		if err != nil {
			t.Fatalf("compute diff: %v", err)
		}
		if diff == nil {
			t.Fatalf("expected diff, got nil")
		}

		var diffs []audited.FieldChange
		if err := json.Unmarshal(diff, &diffs); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		// 验证按字母顺序排列：a_field, name, status, z_field
		expectedOrder := []string{"a_field", "name", "status", "z_field"}
		if len(diffs) != len(expectedOrder) {
			t.Fatalf("expected %d diffs, got %d", len(expectedOrder), len(diffs))
		}
		for j, expected := range expectedOrder {
			if diffs[j].Field != expected {
				t.Fatalf("iteration %d: expected field[%d]=%s, got %s", i, j, expected, diffs[j].Field)
			}
		}
	}
}

// TestComputeEntityDiff_PreservesNumberType 验证 ComputeEntityDiff PreservesNumberType。
func TestComputeEntityDiff_PreservesNumberType(t *testing.T) {
	type testEntity struct {
		Count int64 `json:"count"`
	}

	before := &testEntity{Count: 100}
	after := &testEntity{Count: 200}

	diff, err := computeEntityDiff(before, after)
	if err != nil {
		t.Fatalf("compute diff: %v", err)
	}
	if diff == nil {
		t.Fatalf("expected diff, got nil")
	}

	var diffs []audited.FieldChange
	if err := json.Unmarshal(diff, &diffs); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(diffs) != 1 || diffs[0].Field != "count" {
		t.Fatalf("unexpected diffs: %+v", diffs)
	}

	// 验证 Old/New 保留为整数表示（不会有小数点）
	diffJSON := string(diff)
	if strings.Contains(diffJSON, "100.") || strings.Contains(diffJSON, "200.") {
		t.Fatalf("expected integer representation without decimal, got: %s", diffJSON)
	}
	// json.Number 在最终 Marshal 时会保持原始数值表示
	if !strings.Contains(diffJSON, `"old":100`) || !strings.Contains(diffJSON, `"new":200`) {
		t.Fatalf("expected integer values in diff, got: %s", diffJSON)
	}
}

// TestComputeEntityDiff_FiltersFrameworkFields 验证 ComputeEntityDiff FiltersFrameworkFields。
func TestComputeEntityDiff_FiltersFrameworkFields(t *testing.T) {
	before := &auditedTestEntity{
		AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1, Version: 1}},
		Name:          "old",
		Status:        "pending",
	}
	after := &auditedTestEntity{
		AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1, Version: 2}}, // version 变了
		Name:          "new",
		Status:        "pending",
	}

	diff, err := computeEntityDiff(before, after)
	if err != nil {
		t.Fatalf("compute diff: %v", err)
	}
	if diff == nil {
		t.Fatalf("expected diff, got nil")
	}

	var diffs []audited.FieldChange
	if err := json.Unmarshal(diff, &diffs); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// 只应包含 name 变更，version 应被过滤
	for _, d := range diffs {
		if isAuditIgnoredField(d.Field) {
			t.Fatalf("framework field %q should be filtered from diff", d.Field)
		}
	}

	// 验证 name 变更存在
	found := false
	for _, d := range diffs {
		if d.Field == "name" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected 'name' field in diff, got: %+v", diffs)
	}
}

// TestAuditedApplication_Update_RollsBackWhenAuditStoreFails 验证 AuditedApplication Update RollsBackWhenAuditStoreFails。
func TestAuditedApplication_Update_RollsBackWhenAuditStoreFails(t *testing.T) {
	repo := newMemAuditedRepo()
	store := failingAuditStore{err: errors.NewCode(errors.Internal, "audit store down")}
	app, err := NewApplication(repo, store, nil)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	// 预置实体
	original := &auditedTestEntity{
		AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1, Version: 1}},
		Name:          "original_name",
		Status:        "pending",
	}
	repo.items[1] = cloneAuditedTestEntity(original)

	// 尝试更新
	ctx, err := contextx.WithOperator(context.Background(), "alice")
	if err != nil {
		t.Fatalf("WithOperator returned error: %v", err)
	}
	updated := &auditedTestEntity{
		AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1, Version: 1}},
		Name:          "updated_name",
		Status:        "active",
	}
	if err := app.Update(ctx, updated); err == nil {
		t.Fatalf("expected update to fail when audit store fails")
	}

	// 验证实体未被修改（事务已回滚）
	got, err := repo.Get(context.Background(), 1)
	if err != nil {
		t.Fatalf("get after failed update: %v", err)
	}
	if got.Name != "original_name" {
		t.Fatalf("expected entity to be rolled back, got name=%s", got.Name)
	}
}

// TestAuditedApplication_Delete_RollsBackWhenAuditStoreFails 验证 AuditedApplication Delete RollsBackWhenAuditStoreFails。
func TestAuditedApplication_Delete_RollsBackWhenAuditStoreFails(t *testing.T) {
	repo := newMemAuditedRepo()
	store := failingAuditStore{err: errors.NewCode(errors.Internal, "audit store down")}
	app, err := NewApplication(repo, store, nil)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	// 预置实体
	e := &auditedTestEntity{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1}}}
	repo.items[1] = e

	ctx, err := contextx.WithOperator(context.Background(), "bob")
	if err != nil {
		t.Fatalf("WithOperator returned error: %v", err)
	}
	if err := app.Delete(ctx, 1); err == nil {
		t.Fatalf("expected delete to fail when audit store fails")
	}

	// 验证实体未被软删（事务已回滚）
	got, err := repo.Get(context.Background(), 1)
	if err != nil {
		t.Fatalf("get after failed delete: %v", err)
	}
	if got.IsDeleted() {
		t.Fatalf("expected entity to not be deleted after rollback")
	}
}

// TestAuditedApplication_Restore_RollsBackWhenAuditStoreFails 验证 AuditedApplication Restore RollsBackWhenAuditStoreFails。
func TestAuditedApplication_Restore_RollsBackWhenAuditStoreFails(t *testing.T) {
	repo := newMemAuditedRepo()
	store := failingAuditStore{err: errors.NewCode(errors.Internal, "audit store down")}
	app, err := NewApplication(repo, store, nil)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	// 预置一条已软删的数据
	e := &auditedTestEntity{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1}}}
	_ = e.SoftDeleteBy("alice", time.Now())
	repo.items[1] = e

	if err := app.Restore(context.Background(), 1, "bob"); err == nil {
		t.Fatalf("expected restore to fail when audit store fails")
	}

	// 验证实体仍为已删除状态（事务已回滚）
	got, err := repo.Get(context.Background(), 1)
	if err != nil {
		t.Fatalf("get after failed restore: %v", err)
	}
	if !got.IsDeleted() {
		t.Fatalf("expected entity to remain deleted after rollback")
	}
}
