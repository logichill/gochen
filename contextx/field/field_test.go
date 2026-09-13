package field

import (
	"context"
	"testing"

	"gochen/testkit/require"

	"gochen/errors"
)

func TestCorrelationFieldsTrimWhitespace(t *testing.T) {
	ctx, err := WithTraceID(context.Background(), "\t trace-1 \t")
	require.NoError(t, err)
	require.Equal(t, "trace-1", TraceID(ctx))

	ctx, err = WithRequestID(ctx, "\t req-1 \t")
	require.NoError(t, err)
	require.Equal(t, "req-1", RequestID(ctx))
}

func TestPrincipalFieldsRoundTrip(t *testing.T) {
	ctx, err := WithTenantID(context.Background(), " tenant-1 ")
	require.NoError(t, err)
	ctx, err = WithOperator(ctx, " operator-1 ")
	require.NoError(t, err)
	require.Equal(t, "tenant-1", TenantID(ctx))
	require.Equal(t, "operator-1", Operator(ctx))
}

func TestFieldsRejectNilContext(t *testing.T) {
	_, err := WithTraceID(nil, "trace-1")
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
}
