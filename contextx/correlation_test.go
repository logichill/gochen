package contextx

import (
	stdctx "context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTraceAndRequestContext(t *testing.T) {
	ctx, err := WithTraceID(stdctx.Background(), "trc-1")
	require.NoError(t, err)
	require.Equal(t, "trc-1", TraceID(ctx))

	ctx, err = WithRequestID(ctx, "req-1")
	require.NoError(t, err)
	require.Equal(t, "req-1", RequestID(ctx))
}

func TestDeriveFromMetadata(t *testing.T) {
	md := MapMetadata{
		MetadataTenantKey:    "t1",
		MetadataTraceKey:     "t1-trace",
		MetadataRequestIDKey: "req-1",
		MetadataOperatorKey:  "alice",
	}

	ctx, err := DeriveFromMetadata(stdctx.Background(), md)
	require.NoError(t, err)
	require.Equal(t, "t1", TenantID(ctx))
	require.Equal(t, "t1-trace", TraceID(ctx))
	require.Equal(t, "req-1", RequestID(ctx))
	require.Equal(t, "alice", Operator(ctx))

	ctx2, err := WithTraceID(stdctx.Background(), "t2-trace")
	require.NoError(t, err)
	ctx2, err = DeriveFromMetadata(ctx2, md)
	require.NoError(t, err)
	require.Equal(t, "t2-trace", TraceID(ctx2))
}

func TestEnsureTraceID(t *testing.T) {
	ctx := stdctx.Background()
	md := MapMetadata{}
	ctx, err := EnsureTraceID(ctx, md, "fallback-1")
	require.NoError(t, err)
	require.Equal(t, "fallback-1", TraceID(ctx))
	require.Equal(t, "fallback-1", md[MetadataTraceKey])

	md2 := MapMetadata{MetadataTraceKey: "m1"}
	ctx2, err := EnsureTraceID(stdctx.Background(), md2, "fallback-2")
	require.NoError(t, err)
	require.Equal(t, "m1", TraceID(ctx2))

	base, err := WithTraceID(stdctx.Background(), "t1")
	require.NoError(t, err)
	md3 := MapMetadata{MetadataTraceKey: "md-trace"}
	ctx3, err := EnsureTraceID(base, md3, "fallback-3")
	require.NoError(t, err)
	require.Equal(t, "t1", TraceID(ctx3))
	require.Equal(t, "t1", md3[MetadataTraceKey])
}

func TestInjectAll(t *testing.T) {
	ctx, err := WithTenantID(stdctx.Background(), "t1")
	require.NoError(t, err)
	ctx, err = WithTraceID(ctx, "trc-1")
	require.NoError(t, err)
	ctx, err = WithRequestID(ctx, "req-1")
	require.NoError(t, err)
	ctx, err = WithOperator(ctx, "alice")
	require.NoError(t, err)

	md := MapMetadata{}
	require.NoError(t, InjectAll(ctx, md))
	require.Equal(t, "t1", md[MetadataTenantKey])
	require.Equal(t, "trc-1", md[MetadataTraceKey])
	require.Equal(t, "req-1", md[MetadataRequestIDKey])
	require.Equal(t, "alice", md[MetadataOperatorKey])
}
