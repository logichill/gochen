package audited

import (
	"testing"

	domaudited "gochen/domain/audited"
	"gochen/domain/crud"
	"gochen/errors"
)

func TestVersionsByEntityID_RejectsDuplicateIDs(t *testing.T) {
	entities := []*auditedTestEntity{
		{AuditedEntity: &domaudited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1, Version: 3}}},
		{AuditedEntity: &domaudited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1, Version: 9}}},
		{AuditedEntity: &domaudited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 2, Version: 5}}},
	}

	_, err := versionsByEntityID(entities)
	if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected InvalidInput, got %v", err)
	}
}

func TestEnsureBatchVersionsMatch_ReturnsConcurrencyOnMismatch(t *testing.T) {
	before := map[int64]*auditedTestEntity{
		1: {AuditedEntity: &domaudited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1, Version: 7}}},
	}
	entities := []*auditedTestEntity{
		{AuditedEntity: &domaudited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1, Version: 3}}},
	}

	expectedVersions, err := versionsByEntityID(entities)
	if err != nil {
		t.Fatalf("versionsByEntityID returned error: %v", err)
	}
	err = ensureBatchVersionsMatch(before, expectedVersions, entities)
	if !errors.Is(err, errors.Concurrency) {
		t.Fatalf("expected Concurrency, got %v", err)
	}
}

func TestEnsureBatchVersionsMatch_ReturnsInternalWhenExpectedVersionMissing(t *testing.T) {
	before := map[int64]*auditedTestEntity{
		1: {AuditedEntity: &domaudited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1, Version: 7}}},
	}
	entities := []*auditedTestEntity{
		{AuditedEntity: &domaudited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1, Version: 7}}},
	}

	err := ensureBatchVersionsMatch(before, map[int64]uint64{}, entities)
	if !errors.Is(err, errors.Internal) {
		t.Fatalf("expected Internal, got %v", err)
	}
}
