package testkit_test

import (
	"context"
	"testing"

	"gochen/domain/crud"
	"gochen/domain/spec"
	"gochen/errors"
	"gochen/testkit"
	"gochen/testkit/require"
)

type privateReferenceEntity struct {
	crud.Entity[int64]
	State any
}

func TestMemoryWriteRejectsUncopyableStateWithoutPanicOrMutation(t *testing.T) {
	repo, err := testkit.NewMemoryRepository[*privateReferenceEntity, int64](testkit.NewInt64Sequence(1))
	require.NoError(t, err)
	ctx := context.Background()
	private := struct{ values map[string]string }{map[string]string{"name": "before"}}
	entity := &privateReferenceEntity{State: private}
	require.True(t, errors.Is(repo.Create(ctx, entity, spec.All()), errors.Unsupported))
	require.Equal(t, int64(0), entity.ID)
	require.Equal(t, "before", private.values["name"])
	entity.State = "before"
	require.NoError(t, repo.Create(ctx, entity, spec.All()))
	entity.State = private
	require.True(t, errors.Is(repo.Update(ctx, entity, spec.All()), errors.Unsupported))
	stored, err := repo.Get(ctx, entity.ID, spec.All())
	require.NoError(t, err)
	require.Equal(t, "before", stored.State)
	require.Equal(t, uint64(0), stored.Version)
}
