package testkit

import (
	"context"
	"testing"
	"time"

	"gochen/domain/meta"
	"gochen/domain/spec"
	"gochen/errors"
	"gochen/testkit/require"
)

func TestMemoryRestoreChecksPostImageBeforeWriting(t *testing.T) {
	ctx := context.Background()
	repository, err := NewAuditedMemoryRepository[*auditedMemoryEntity, int64](NewInt64Sequence(1))
	require.NoError(t, err)
	entity := &auditedMemoryEntity{Name: "open"}
	require.NoError(t, repository.Create(ctx, entity, spec.All()))
	require.NoError(t, entity.SoftDeleteBy("alice", time.Unix(1, 0).UTC()))
	require.NoError(t, repository.Update(ctx, entity, spec.All()))
	source := spec.Eq(meta.ByField("Name"), "open")
	candidate, err := repository.GetWithDeleted(ctx, entity.ID, source)
	require.NoError(t, err)
	expectedVersion := candidate.Version
	require.NoError(t, candidate.Restore())
	candidate.Name = "closed"
	err = repository.Restore(ctx, candidate, source)
	require.True(t, errors.Is(err, errors.Forbidden), "post-image outside spec must be rejected")
	require.Equal(t, expectedVersion, candidate.Version)
	stored, err := repository.GetWithDeleted(ctx, entity.ID, spec.All())
	require.NoError(t, err)
	require.Equal(t, "open", stored.Name)
	require.True(t, stored.IsDeleted())
	require.Equal(t, expectedVersion, stored.Version)

	require.NoError(t, stored.Restore())
	require.NoError(t, repository.Restore(ctx, stored, source))
	restored, err := repository.Get(ctx, entity.ID, source)
	require.NoError(t, err)
	require.False(t, restored.IsDeleted())
	require.Equal(t, expectedVersion+1, restored.Version)
}
