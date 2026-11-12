package auth_test

import (
	"context"
	"testing"

	"gochen/auth"

	"gochen/errors"
	"gochen/testkit/require"
)

func TestSnapshotRequiresNonemptyVersion(t *testing.T) {
	for _, version := range []string{"", " \t\n", " v1 "} {
		for _, lenient := range []bool{false, true} {
			resolver := auth.PolicySnapshotFunc(func(context.Context) (auth.PolicySnapshot, error) {
				return auth.PolicySnapshot{Version: version}, nil
			})
			authorizer, err := auth.New(auth.Options{Policy: auth.AllowPolicy(), Governance: auth.GovernanceOptions{Snapshot: resolver, LenientSnapshot: lenient}})
			require.NoError(t, err)
			decision, err := authorizer.Authorize(context.Background(), auth.Request{})
			if version == " v1 " {
				require.NoError(t, err)
				require.Equal(t, "v1", decision.SnapshotVersion)
			} else if lenient {
				require.NoError(t, err)
				require.True(t, decision.Allowed)
				require.Equal(t, "", decision.SnapshotVersion)
			} else {
				require.True(t, errors.Is(err, errors.Forbidden))
				require.False(t, decision.Allowed)
				require.Equal(t, "snapshot_unavailable", decision.Reason)
			}
		}
	}
}
