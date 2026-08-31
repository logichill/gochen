package contextx

import (
	"context"
	"testing"

	"gochen/contextx/propagation"

	"gochen/testkit/require"
)

func TestTraceIDContextTrimsOnWriteAndRead(t *testing.T) {
	ctx, err := WithTraceID(context.Background(), "  trace-1  ")
	require.NoError(t, err)
	require.Equal(t, "trace-1", TraceID(ctx))
}

func TestInjectTenantAndTraceOverwriteBlankMetadata(t *testing.T) {
	ctx, err := WithTenantID(context.Background(), "  tenant-1  ")
	require.NoError(t, err)
	ctx, err = WithTraceID(ctx, "  trace-1  ")
	require.NoError(t, err)

	metadata := propagation.MapCarrier{
		MetadataTenantKey: "   ",
		MetadataTraceKey:  "\t",
	}

	require.NoError(t, InjectTenantID(ctx, metadata))
	require.NoError(t, InjectTraceID(ctx, metadata))
	require.Equal(t, "tenant-1", metadata[MetadataTenantKey])
	require.Equal(t, "trace-1", metadata[MetadataTraceKey])
}

func TestInjectTenantAndOperatorPreferContextOverConflictingMetadata(t *testing.T) {
	ctx, err := WithTenantID(context.Background(), "tenant-ctx")
	require.NoError(t, err)
	ctx, err = WithOperator(ctx, "alice")
	require.NoError(t, err)

	metadata := propagation.MapCarrier{
		MetadataTenantKey:   "tenant-stale",
		MetadataOperatorKey: "bob",
	}

	require.NoError(t, InjectTenantID(ctx, metadata))
	require.NoError(t, InjectOperator(ctx, metadata))
	require.Equal(t, "tenant-ctx", metadata[MetadataTenantKey])
	require.Equal(t, "alice", metadata[MetadataOperatorKey])
}

func TestInjectTenantKeepsMetadataWhenContextTenantMissing(t *testing.T) {
	metadata := propagation.MapCarrier{MetadataTenantKey: "tenant-metadata"}

	require.NoError(t, InjectTenantID(context.Background(), metadata))
	require.Equal(t, "tenant-metadata", metadata[MetadataTenantKey])
}

func TestMapCarrierSetNilMapPanics(t *testing.T) {
	var metadata propagation.MapCarrier
	require.PanicsWithValue(t, "propagation.MapCarrier.Set called on nil map; use NewMapCarrier", func() {
		metadata.Set(MetadataTraceKey, "trace-1")
	})
}

func TestNewMapCarrierCreatesWritableMetadata(t *testing.T) {
	metadata := propagation.NewMapCarrier()
	metadata.Set(MetadataTraceKey, "trace-1")
	require.Equal(t, "trace-1", metadata[MetadataTraceKey])
}

func TestDeriveFromMetadataTrimsTenantAndTrace(t *testing.T) {
	ctx, err := DeriveFromMetadata(context.Background(), propagation.MapCarrier{
		MetadataTenantKey: "  tenant-1  ",
		MetadataTraceKey:  "  trace-1  ",
	})
	require.NoError(t, err)

	require.Equal(t, "tenant-1", TenantID(ctx))
	require.Equal(t, "trace-1", TraceID(ctx))
}
