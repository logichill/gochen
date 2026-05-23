package contextx

import (
	stdctx "context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUserIDContextKey(t *testing.T) {
	ctx, err := WithUserID(stdctx.Background(), 42)
	require.NoError(t, err)
	require.Equal(t, int64(42), UserID(ctx))
}

func TestSessionVisibility(t *testing.T) {
	ctx, err := WithSessionID(stdctx.Background(), "sess-1")
	require.NoError(t, err)
	require.Equal(t, "sess-1", SessionID(ctx))

	ctx, err = WithSessionVisibility(ctx, false)
	require.NoError(t, err)
	require.Equal(t, "", SessionID(ctx))
	require.False(t, SessionVisible(ctx))

	ctx, err = WithSessionVisibility(ctx, true)
	require.NoError(t, err)
	require.Equal(t, "sess-1", SessionID(ctx))
	require.True(t, SessionVisible(ctx))
}

func TestOperatorAndTenantContext(t *testing.T) {
	ctx, err := WithTenantID(stdctx.Background(), "t1")
	require.NoError(t, err)
	require.Equal(t, "t1", TenantID(ctx))

	ctx, err = WithOperator(ctx, "  alice  ")
	require.NoError(t, err)
	require.Equal(t, "alice", Operator(ctx))
}
