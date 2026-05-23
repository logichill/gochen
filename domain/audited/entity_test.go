package audited

import (
	"testing"
	"time"

	"gochen/domain/crud"
	gerrors "gochen/errors"
)

// TestAuditedEntity_SoftDeleteAndRestore 验证 AuditedEntity SoftDeleteAndRestore。
func TestAuditedEntity_SoftDeleteAndRestore(t *testing.T) {
	e := &AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1, Version: 7}}

	if e.IsDeleted() {
		t.Fatalf("expected not deleted initially")
	}
	if e.GetDeletedAt() != nil || e.GetDeletedBy() != nil {
		t.Fatalf("expected deleted fields to be nil initially")
	}

	at := time.Now()
	if err := e.SoftDelete(at); err != nil {
		t.Fatalf("SoftDelete failed: %v", err)
	}
	if !e.IsDeleted() {
		t.Fatalf("expected IsDeleted()=true after SoftDelete")
	}
	if e.GetDeletedAt() == nil || !e.GetDeletedAt().Equal(at) {
		t.Fatalf("unexpected DeletedAt: %v, want %v", e.GetDeletedAt(), at)
	}
	if e.GetDeletedBy() != nil {
		t.Fatalf("unexpected DeletedBy: %v, want nil", e.GetDeletedBy())
	}
	if e.GetVersion() != 7 {
		t.Fatalf("expected Version unchanged by SoftDelete, got %d", e.GetVersion())
	}

	err := e.SoftDelete(at)
	if err == nil {
		t.Fatalf("expected conflict on second SoftDelete, got nil")
	}
	if !gerrors.Is(err, gerrors.Conflict) {
		t.Fatalf("expected Conflict, got: %v", err)
	}
	var appErr *gerrors.AppError
	if !gerrors.As(err, &appErr) || appErr == nil {
		t.Fatalf("expected *errors.AppError, got: %T", err)
	}
	if got := appErr.Details()["id"]; got != int64(1) {
		t.Fatalf("expected conflict error to include id=1, got %v", got)
	}

	t.Run("SoftDeleteBy requires operator", func(t *testing.T) {
		e := &AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1}}
		err := e.SoftDeleteBy("   ", time.Now())
		if err == nil {
			t.Fatalf("expected invalid input, got nil")
		}
		if !gerrors.Is(err, gerrors.InvalidInput) {
			t.Fatalf("expected InvalidInput, got: %v", err)
		}
	})

	t.Run("SoftDeleteBy records operator and timestamp", func(t *testing.T) {
		e := &AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1, Version: 7}}
		at := time.Now()
		if err := e.SoftDeleteBy("alice", at); err != nil {
			t.Fatalf("SoftDeleteBy failed: %v", err)
		}
		if !e.IsDeleted() {
			t.Fatalf("expected IsDeleted()=true after SoftDeleteBy")
		}
		if e.GetDeletedAt() == nil || !e.GetDeletedAt().Equal(at) {
			t.Fatalf("unexpected DeletedAt: %v, want %v", e.GetDeletedAt(), at)
		}
		if e.GetDeletedBy() == nil || *e.GetDeletedBy() != "alice" {
			t.Fatalf("unexpected DeletedBy: %v, want alice", e.GetDeletedBy())
		}
		if e.GetVersion() != 7 {
			t.Fatalf("expected Version unchanged by SoftDeleteBy, got %d", e.GetVersion())
		}
	})

	if err := e.Restore(); err != nil {
		t.Fatalf("Restore failed: %v", err)
	}
	if e.IsDeleted() {
		t.Fatalf("expected IsDeleted()=false after Restore")
	}
	if e.GetDeletedAt() != nil || e.GetDeletedBy() != nil {
		t.Fatalf("expected deleted fields to be nil after Restore")
	}
	if e.GetVersion() != 7 {
		t.Fatalf("expected Version unchanged by Restore, got %d", e.GetVersion())
	}

	err = e.Restore()
	if err == nil {
		t.Fatalf("expected conflict on Restore when not deleted, got nil")
	}
	if !gerrors.Is(err, gerrors.Conflict) {
		t.Fatalf("expected Conflict, got: %v", err)
	}
}

// TestAuditedEntity_SetUpdatedInfo_IncrementsVersion 验证 AuditedEntity SetUpdatedInfo IncrementsVersion。
func TestAuditedEntity_SetUpdatedInfo_IncrementsVersion(t *testing.T) {
	e := &AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1, Version: 7}}
	at := time.Now()

	e.SetUpdatedInfo("alice", at)

	if e.GetUpdatedBy() != "alice" {
		t.Fatalf("unexpected UpdatedBy: %s", e.GetUpdatedBy())
	}
	if !e.GetUpdatedAt().Equal(at) {
		t.Fatalf("unexpected UpdatedAt: %v, want %v", e.GetUpdatedAt(), at)
	}
	if e.GetVersion() != 8 {
		t.Fatalf("expected Version incremented to 8, got %d", e.GetVersion())
	}
}

// TestAuditedEntity_ImplementsITimestamps 验证 AuditedEntity ImplementsITimestamps。
func TestAuditedEntity_ImplementsITimestamps(t *testing.T) {
	// 编译期已检查，这里做运行时验证
	e := &AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1}}

	now := time.Now()

	// 测试 SetCreatedAt
	e.SetCreatedAt(now)
	if !e.GetCreatedAt().Equal(now) {
		t.Fatalf("SetCreatedAt failed: expected %v, got %v", now, e.GetCreatedAt())
	}

	// 测试 SetUpdatedAt（不递增版本）
	later := now.Add(time.Hour)
	versionBefore := e.GetVersion()
	e.SetUpdatedAt(later)
	if !e.GetUpdatedAt().Equal(later) {
		t.Fatalf("SetUpdatedAt failed: expected %v, got %v", later, e.GetUpdatedAt())
	}
	if e.GetVersion() != versionBefore {
		t.Fatalf("SetUpdatedAt should not increment version: expected %d, got %d", versionBefore, e.GetVersion())
	}
}

// TestAuditedEntity_SetCreatedInfo 验证 AuditedEntity SetCreatedInfo。
func TestAuditedEntity_SetCreatedInfo(t *testing.T) {
	e := &AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1}}
	at := time.Now()

	e.SetCreatedInfo("bob", at)

	if e.GetCreatedBy() != "bob" {
		t.Fatalf("unexpected CreatedBy: %s", e.GetCreatedBy())
	}
	if !e.GetCreatedAt().Equal(at) {
		t.Fatalf("unexpected CreatedAt: %v, want %v", e.GetCreatedAt(), at)
	}
}

// TestAuditedEntity_Validate 验证 AuditedEntity Validate。
func TestAuditedEntity_Validate(t *testing.T) {
	e := &AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1}}

	if err := e.Validate(); err != nil {
		t.Fatalf("default Validate should return nil, got: %v", err)
	}
}
