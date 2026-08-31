package testkit

import (
	"context"
	stderrors "errors"
	"fmt"
	"testing"
	"time"

	"gochen/domain/audited"
	"gochen/domain/crud"
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

	if err := repository.CreateAll(ctx, []*memoryEntity{first, second}); err != nil {
		t.Fatalf("create all: %v", err)
	}
	if first.ID != 1 || second.ID != 2 {
		t.Fatalf("generated IDs = %d, %d; want 1, 2", first.ID, second.ID)
	}

	page, err := repository.List(ctx, 1, 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page) != 1 || page[0].Name != "second" {
		t.Fatalf("page = %#v; want second entity", page)
	}

	page[0].Name = "caller mutation"
	stored, err := repository.Get(ctx, second.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.Name != "second" {
		t.Fatalf("stored name = %q; caller mutated repository state", stored.Name)
	}

	stored.Name = "updated"
	if err := repository.Update(ctx, stored); err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := repository.Delete(ctx, first.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	count, err := repository.Count(ctx)
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

	if err := repository.Create(ctx, entity); err != nil {
		t.Fatalf("create: %v", err)
	}
	if entity.ID != "entity-1" {
		t.Fatalf("ID = %q; want entity-1", entity.ID)
	}
	if _, err := repository.Get(ctx, "entity-1"); err != nil {
		t.Fatalf("get generated string ID: %v", err)
	}
}

func TestMemoryRepositoryBatchUpdateIsAtomic(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryRepository[*memoryEntity, int64](NewInt64Sequence(1))
	first := &memoryEntity{Name: "first"}
	if err := repository.Create(ctx, first); err != nil {
		t.Fatalf("create: %v", err)
	}

	err := repository.UpdateAll(ctx, []*memoryEntity{
		{Entity: crud.Entity[int64]{ID: first.ID}, Name: "changed"},
		{Entity: crud.Entity[int64]{ID: 99}, Name: "missing"},
	})
	if !errors.Is(err, errors.NotFound) {
		t.Fatalf("update all error = %v; want NotFound", err)
	}
	stored, getErr := repository.Get(ctx, first.ID)
	if getErr != nil {
		t.Fatalf("get: %v", getErr)
	}
	if stored.Name != "first" {
		t.Fatalf("stored name = %q; failed batch partially applied", stored.Name)
	}
}

func TestMemoryRepositoryTransactionRollbackAndConflict(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryRepository[*memoryEntity, int64](NewInt64Sequence(1))
	wantErr := stderrors.New("rollback")

	err := repository.WithinTx(ctx, func(txCtx context.Context) error {
		if err := repository.Create(txCtx, &memoryEntity{Name: "rolled back"}); err != nil {
			return err
		}
		return wantErr
	})
	if !stderrors.Is(err, wantErr) {
		t.Fatalf("transaction error = %v; want rollback marker", err)
	}
	count, err := repository.Count(ctx)
	if err != nil || count != 0 {
		t.Fatalf("count after rollback = %d, err = %v; want 0, nil", count, err)
	}

	scope, err := repository.BeginTx(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := repository.Create(scope.Context(), &memoryEntity{Name: "transaction"}); err != nil {
		t.Fatalf("transaction create: %v", err)
	}
	if err := repository.Create(ctx, &memoryEntity{Name: "concurrent"}); err != nil {
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
	if err := repository.Create(ctx, entity); err != nil {
		t.Fatalf("create: %v", err)
	}

	stored, err := repository.Get(ctx, entity.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if err := stored.SoftDeleteBy("tester", time.Unix(1, 0).UTC()); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	if err := repository.Update(ctx, stored); err != nil {
		t.Fatalf("update deleted entity: %v", err)
	}
	if _, err := repository.Get(ctx, entity.ID); !errors.Is(err, errors.NotFound) {
		t.Fatalf("get deleted error = %v; want NotFound", err)
	}
	deleted, err := repository.ListDeleted(ctx, 0, 10)
	if err != nil || len(deleted) != 1 {
		t.Fatalf("deleted list length = %d, err = %v; want 1, nil", len(deleted), err)
	}

	marker := stderrors.New("abort audited transaction")
	err = repository.WithinTx(ctx, func(txCtx context.Context) error {
		_, err := repository.SaveAuditRecord(txCtx, audited.AuditRecord{
			EntityID:  "1",
			Operation: audited.AuditOpDelete,
			Operator:  "tester",
		})
		if err != nil {
			return err
		}
		return marker
	})
	if !stderrors.Is(err, marker) {
		t.Fatalf("transaction error = %v; want marker", err)
	}
	records, err := repository.ListAuditRecordsByEntity(ctx, "1", 0, 10)
	if err != nil || len(records) != 0 {
		t.Fatalf("records after rollback = %d, err = %v; want 0, nil", len(records), err)
	}
}
