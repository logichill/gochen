package access

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWriteConstraint_RequireResource(t *testing.T) {
	constraint := WriteConstraint{
		Resources: []ResourceConstraint{
			{Kind: "orders", ResourceID: "1", ManagedScopeID: 10},
			{Kind: "orders", ResourceID: "2", ManagedScopeID: 20},
		},
	}

	t.Run("matches single resource by kind and ID", func(t *testing.T) {
		res, err := constraint.RequireResource("orders", "1")
		require.NoError(t, err)
		require.Equal(t, "1", res.ResourceID)
		require.Equal(t, int64(10), res.ManagedScopeID)
	})

	t.Run("rejects unknown resource ID", func(t *testing.T) {
		_, err := constraint.RequireResource("orders", "99")
		require.Error(t, err)
	})

	t.Run("rejects unknown kind", func(t *testing.T) {
		_, err := constraint.RequireResource("users", "1")
		require.Error(t, err)
	})

	t.Run("empty constraint returns error", func(t *testing.T) {
		_, err := WriteConstraint{}.RequireResource("orders", "1")
		require.Error(t, err)
	})
}

func TestWriteConstraint_SplitByTargets(t *testing.T) {
	constraint := WriteConstraint{
		Resources: []ResourceConstraint{
			{Kind: "orders", ResourceID: "1"},
			{Kind: "orders", ResourceID: "2"},
		},
	}

	t.Run("splits by matching targets", func(t *testing.T) {
		constraint.Metadata = ConstraintMetadata{
			DecisionID:      "decision-1",
			SnapshotVersion: "snap-1",
			Consistency:     "strong",
		}
		targets := []ResourceBoundary{
			{Kind: "orders", ID: "2"},
			{Kind: "orders", ID: "1"},
		}
		split, err := constraint.SplitByTargets(targets)
		require.NoError(t, err)
		require.Len(t, split, 2)
		require.Equal(t, "2", split[0].Resources[0].ResourceID)
		require.Equal(t, "1", split[1].Resources[0].ResourceID)
		require.Equal(t, constraint.Metadata, split[0].Metadata)
		require.Equal(t, constraint.Metadata, split[1].Metadata)
	})

	t.Run("rejects count mismatch", func(t *testing.T) {
		targets := []ResourceBoundary{{Kind: "orders", ID: "1"}}
		_, err := constraint.SplitByTargets(targets)
		require.Error(t, err)
	})

	t.Run("empty targets returns nil", func(t *testing.T) {
		split, err := constraint.SplitByTargets(nil)
		require.NoError(t, err)
		require.Nil(t, split)
	})
}

func TestWriteConstraint_ScopedContext(t *testing.T) {
	t.Run("binds managed scope to context", func(t *testing.T) {
		constraint := WriteConstraint{
			Resources: []ResourceConstraint{{Kind: "orders", ResourceID: "1", ManagedScopeID: 42}},
		}
		ctx, err := constraint.ScopedContext(context.Background())
		require.NoError(t, err)
		scope, ok := DataScopeFromContext(ctx)
		require.True(t, ok)
		require.Equal(t, int64(42), scope.ActiveScopeID)
	})

	t.Run("returns ctx unchanged when ManagedScopeID is 0", func(t *testing.T) {
		constraint := WriteConstraint{
			Resources: []ResourceConstraint{{Kind: "orders", ResourceID: "1"}},
		}
		ctx, err := constraint.ScopedContext(context.Background())
		require.NoError(t, err)
		_, ok := DataScopeFromContext(ctx)
		require.False(t, ok)
	})

	t.Run("rejects multi-resource constraint", func(t *testing.T) {
		constraint := WriteConstraint{
			Resources: []ResourceConstraint{
				{Kind: "orders", ResourceID: "1"},
				{Kind: "orders", ResourceID: "2"},
			},
		}
		_, err := constraint.ScopedContext(context.Background())
		require.Error(t, err)
	})

	t.Run("rejects empty constraint", func(t *testing.T) {
		_, err := WriteConstraint{}.ScopedContext(context.Background())
		require.Error(t, err)
	})
}
