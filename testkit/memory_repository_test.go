package testkit

import (
	"context"
	stderrors "errors"
	"fmt"
	"testing"
	"time"

	"gochen/domain/audited"
	"gochen/domain/crud"
	"gochen/domain/spec"
	"gochen/errors"
)

type memoryEntity struct {
	crud.Entity[int64]
	Name string
}

type stringMemoryEntity struct {
	crud.Entity[string]
	Name string
}

type auditedMemoryEntity struct {
	audited.AuditedEntity[int64]
	Name string
}

func TestMemoryRepositoryCRUDAndStablePagination(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryRepository[*memoryEntity, int64](NewInt64Sequence(1))
	first := &memoryEntity{Name: "first"}
	second := &memoryEntity{Name: "second"}

	if err := repository.CreateBulk(ctx, []*memoryEntity{first, second}, spec.All()); err != nil {
		t.Fatalf("create all: %v", err)
	}
	if first.ID != 1 || second.ID != 2 {
		t.Fatalf("generated IDs = %d, %d; want 1, 2", first.ID, second.ID)
	}

	page, err := repository.List(ctx, 1, 1, spec.All())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page) != 1 || page[0].Name != "second" {
		t.Fatalf("page = %#v; want second entity", page)
	}

	page[0].Name = "caller mutation"
	stored, err := repository.Get(ctx, second.ID, spec.All())
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.Name != "second" {
		t.Fatalf("stored name = %q; caller mutated repository state", stored.Name)
	}

	stored.Name = "updated"
	if err := repository.Update(ctx, stored, spec.All()); err != nil {
		t.Fatalf("update: %v", err)
	}
	deleteTarget, err := repository.Get(ctx, first.ID, spec.All())
	if err != nil {
		t.Fatalf("get delete target: %v", err)
	}
	if err := repository.Delete(ctx, deleteTarget, spec.All()); err != nil {
		t.Fatalf("delete: %v", err)
	}
	count, err := repository.Count(ctx, spec.All())
	if err != nil || count != 1 {
		t.Fatalf("count = %d, err = %v; want 1, nil", count, err)
	}
}

func TestMemoryRepositorySupportsGenericIDs(t *testing.T) {
	ctx := context.Background()
	next := 0
	repository := NewMemoryRepository[*stringMemoryEntity, string](GeneratorFunc[string](func() (string, error) {
		next++
		return fmt.Sprintf("entity-%d", next), nil
	}))
	entity := &stringMemoryEntity{Name: "generic"}

	if err := repository.Create(ctx, entity, spec.All()); err != nil {
		t.Fatalf("create: %v", err)
	}
	if entity.ID != "entity-1" {
		t.Fatalf("ID = %q; want entity-1", entity.ID)
	}
	if _, err := repository.Get(ctx, "entity-1", spec.All()); err != nil {
		t.Fatalf("get generated string ID: %v", err)
	}
}

func TestMemoryRepositoryBulkUpdateIsAtomic(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryRepository[*memoryEntity, int64](NewInt64Sequence(1))
	first := &memoryEntity{Name: "first"}
	if err := repository.Create(ctx, first, spec.All()); err != nil {
		t.Fatalf("create: %v", err)
	}

	err := repository.UpdateBulk(ctx, []*memoryEntity{
		{Entity: crud.Entity[int64]{ID: first.ID}, Name: "changed"},
		{Entity: crud.Entity[int64]{ID: 99}, Name: "missing"},
	}, spec.All())
	if !errors.Is(err, errors.NotFound) {
		t.Fatalf("update all error = %v; want NotFound", err)
	}
	stored, getErr := repository.Get(ctx, first.ID, spec.All())
	if getErr != nil {
		t.Fatalf("get: %v", getErr)
	}
	if stored.Name != "first" {
		t.Fatalf("stored name = %q; failed bulk partially applied", stored.Name)
	}
}

func TestMemoryRepositoryTransactionRollbackAndConflict(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryRepository[*memoryEntity, int64](NewInt64Sequence(1))
	wantErr := stderrors.New("rollback")

	err := repository.WithinTx(ctx, func(txCtx context.Context) error {
		if err := repository.Create(txCtx, &memoryEntity{Name: "rolled back"}, spec.All()); err != nil {
			return err
		}
		return wantErr
	})
	if !stderrors.Is(err, wantErr) {
		t.Fatalf("transaction error = %v; want rollback marker", err)
	}
	count, err := repository.Count(ctx, spec.All())
	if err != nil || count != 0 {
		t.Fatalf("count after rollback = %d, err = %v; want 0, nil", count, err)
	}

	scope, err := repository.BeginTx(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := repository.Create(scope.Context(), &memoryEntity{Name: "transaction"}, spec.All()); err != nil {
		t.Fatalf("transaction create: %v", err)
	}
	if err := repository.Create(ctx, &memoryEntity{Name: "concurrent"}, spec.All()); err != nil {
		t.Fatalf("concurrent create: %v", err)
	}
	if err := repository.Commit(scope); !errors.Is(err, errors.Concurrency) {
		t.Fatalf("commit error = %v; want Concurrency", err)
	}
	if err := repository.Rollback(scope); err != nil {
		t.Fatalf("rollback conflicted transaction: %v", err)
	}
}

func TestAuditedMemoryRepositoryVisibilityAndAtomicAudit(t *testing.T) {
	ctx := context.Background()
	repository := NewAuditedMemoryRepository[*auditedMemoryEntity, int64](NewInt64Sequence(1))
	entity := &auditedMemoryEntity{Name: "article"}
	if err := repository.Create(ctx, entity, spec.All()); err != nil {
		t.Fatalf("create: %v", err)
	}

	stored, err := repository.Get(ctx, entity.ID, spec.All())
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if err := stored.SoftDeleteBy("tester", time.Unix(1, 0).UTC()); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	// 软删除由 Application 以“读取 → SoftDeleteBy → Update”表达，仓储 Update 必须接受这次落库。
	if err := repository.Update(ctx, stored, spec.All()); err != nil {
		t.Fatalf("update deleted entity: %v", err)
	}
	if _, err := repository.Get(ctx, entity.ID, spec.All()); !errors.Is(err, errors.NotFound) {
		t.Fatalf("get deleted error = %v; want NotFound", err)
	}
	deleted, err := repository.ListDeleted(ctx, 0, 10, spec.All())
	if err != nil || len(deleted) != 1 {
		t.Fatalf("deleted list length = %d, err = %v; want 1, nil", len(deleted), err)
	}

	marker := stderrors.New("abort audited transaction")
	err = repository.WithinTx(ctx, func(txCtx context.Context) error {
		_, err := repository.SaveAuditRecord(txCtx, audited.AuditRecord{
			EntityID: "1",
			Action:   "delete",
			Operator: "tester",
		})
		if err != nil {
			return err
		}
		return marker
	})
	if !stderrors.Is(err, marker) {
		t.Fatalf("transaction error = %v; want marker", err)
	}
	records, err := repository.ListAuditRecords(ctx, "1", 0, 10, spec.All())
	if err != nil || len(records) != 0 {
		t.Fatalf("records after rollback = %d, err = %v; want 0, nil", len(records), err)
	}
}
