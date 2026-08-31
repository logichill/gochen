package contextx

import (
	"context"
	"testing"

	"gochen/errors"
	"gochen/testkit/require"
)

func TestCrossIsolationRoundTrip(t *testing.T) {
	ctx, err := WithCrossIsolation(context.Background(), CrossIsolation{
		Reason:   "  monthly_billing_reconciliation  ",
		Operator: "  platform_cron_worker  ",
	})
	require.NoError(t, err)

	credential, ok := CrossIsolationFrom(ctx)
	require.True(t, ok)
	require.Equal(t, "monthly_billing_reconciliation", credential.Reason)
	require.Equal(t, "platform_cron_worker", credential.Operator)
}

func TestCrossIsolationAbsentByDefault(t *testing.T) {
	_, ok := CrossIsolationFrom(context.Background())
	require.False(t, ok)

	_, ok = CrossIsolationFrom(nil)
	require.False(t, ok)
}

// 留不下理由或留不下发起人的开闸，与静默开闸没有区别，因此构造期就拒绝。
func TestCrossIsolationRequiresReasonAndOperator(t *testing.T) {
	_, err := WithCrossIsolation(context.Background(), CrossIsolation{Operator: "worker"})
	require.True(t, errors.Is(err, errors.InvalidInput))

	_, err = WithCrossIsolation(context.Background(), CrossIsolation{Reason: "audit"})
	require.True(t, errors.Is(err, errors.InvalidInput))

	_, err = WithCrossIsolation(context.Background(), CrossIsolation{Reason: "   ", Operator: "   "})
	require.True(t, errors.Is(err, errors.InvalidInput))
}
