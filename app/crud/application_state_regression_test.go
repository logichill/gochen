package crud

import (
	"context"
	"testing"

	"gochen/auth"
	domcrud "gochen/domain/crud"
	"gochen/errors"
	"gochen/testkit"
	"gochen/testkit/require"
)

type maskedValue string

func (maskedValue) MarshalJSON() ([]byte, error) { return []byte(`"masked"`), nil }

type stateEntity struct {
	domcrud.Entity[int64]
	Secret maskedValue
	Values map[string][]string
}

func TestUpdateComparesActualBusinessState(t *testing.T) {
	for _, mode := range []string{"update", "execute", "bulk"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			repo, err := testkit.NewMemoryRepository[*stateEntity, int64](testkit.NewInt64Sequence(1))
			require.NoError(t, err)
			app, err := NewApplication(repo, Options[*stateEntity, int64]{DeleteMode: DeletePurge, Resource: "doc", Authorizer: auth.Allow()})
			require.NoError(t, err)
			entity := &stateEntity{Secret: "old", Values: map[string][]string{"key": {"before"}}}
			require.NoError(t, app.Create(ctx, entity))
			change := func(_ context.Context, candidate *stateEntity) error {
				candidate.Secret = "new"
				return nil
			}
			switch mode {
			case "update":
				require.NoError(t, change(ctx, entity))
				err = app.Update(ctx, entity)
			case "execute":
				_, err = app.Execute(ctx, ActionUpdate, entity.ID, change)
			case "bulk":
				_, err = app.ExecuteBulk(ctx, ActionUpdate, []int64{entity.ID}, change)
			}
			require.NoError(t, err)
			stored, err := app.Get(ctx, entity.ID)
			require.NoError(t, err)
			require.Equal(t, maskedValue("new"), stored.Secret)
			require.Equal(t, "before", stored.Values["key"][0])
			require.Equal(t, uint64(1), stored.Version)
			_, err = app.Execute(ctx, ActionUpdate, entity.ID, func(_ context.Context, candidate *stateEntity) error {
				candidate.Values["key"][0] = "after"
				return nil
			})
			require.NoError(t, err)
			stored, err = app.Get(ctx, entity.ID)
			require.NoError(t, err)
			require.Equal(t, "after", stored.Values["key"][0])
			require.Equal(t, uint64(2), stored.Version)
			require.NoError(t, app.Update(ctx, stored))
			require.Equal(t, uint64(2), stored.Version)
			stored.Version = 0
			require.True(t, errors.Is(app.Update(ctx, stored), errors.Concurrency))
		})
	}
}

func TestFailedCommandRollsBackNestedBusinessState(t *testing.T) {
	ctx := context.Background()
	repo, err := testkit.NewMemoryRepository[*stateEntity, int64](testkit.NewInt64Sequence(1))
	require.NoError(t, err)
	app, err := NewApplication(repo, Options[*stateEntity, int64]{DeleteMode: DeletePurge, Resource: "doc", Authorizer: auth.Allow()})
	require.NoError(t, err)
	entity := &stateEntity{Values: map[string][]string{"key": {"before"}}}
	require.NoError(t, app.Create(ctx, entity))
	abort := errors.NewCode(errors.Internal, "abort")
	_, err = app.Execute(ctx, ActionUpdate, entity.ID, func(_ context.Context, candidate *stateEntity) error {
		candidate.Values["key"][0] = "leaked"
		return abort
	})
	require.True(t, errors.Is(err, abort))
	stored, err := app.Get(ctx, entity.ID)
	require.NoError(t, err)
	require.Equal(t, "before", stored.Values["key"][0])
	require.Equal(t, uint64(0), stored.Version)
}
