package http

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWithCorrelationID 测试设置 correlation_id
func TestWithCorrelationID(t *testing.T) {
	ctx := context.Background()
	correlationID := "cor-123"

	ctx = WithCorrelationID(ctx, correlationID)
	result := GetCorrelationID(ctx)

	assert.Equal(t, correlationID, result)
}

// TestGetCorrelationID_NotExists 测试获取不存在的 correlation_id
func TestGetCorrelationID_NotExists(t *testing.T) {
	ctx := context.Background()
	result := GetCorrelationID(ctx)

	assert.Empty(t, result)
}

// TestGetCorrelationID_NilContext 测试 nil context
func TestGetCorrelationID_NilContext(t *testing.T) {
	result := GetCorrelationID(nil)
	assert.Empty(t, result)
}

// TestWithCausationID 测试设置 causation_id
func TestWithCausationID(t *testing.T) {
	ctx := context.Background()
	causationID := "cau-456"

	ctx = WithCausationID(ctx, causationID)
	result := GetCausationID(ctx)

	assert.Equal(t, causationID, result)
}

// TestGetCausationID_NotExists 测试获取不存在的 causation_id
func TestGetCausationID_NotExists(t *testing.T) {
	ctx := context.Background()
	result := GetCausationID(ctx)

	assert.Empty(t, result)
}

// TestBothIDs 测试同时设置两个 ID
func TestBothIDs(t *testing.T) {
	ctx := context.Background()
	correlationID := "cor-123"
	causationID := "cau-456"

	ctx = WithCorrelationID(ctx, correlationID)
	ctx = WithCausationID(ctx, causationID)

	assert.Equal(t, correlationID, GetCorrelationID(ctx))
	assert.Equal(t, causationID, GetCausationID(ctx))
}

// TestGenerateCorrelationID 测试生成 correlation_id
func TestGenerateCorrelationID(t *testing.T) {
	id1 := GenerateCorrelationID()
	id2 := GenerateCorrelationID()

	assert.NotEmpty(t, id1)
	assert.NotEmpty(t, id2)
	assert.NotEqual(t, id1, id2) // 应该是唯一的
	assert.Contains(t, id1, "cor-")
}

// TestGenerateCausationID 测试生成 causation_id
func TestGenerateCausationID(t *testing.T) {
	id1 := GenerateCausationID()
	id2 := GenerateCausationID()

	assert.NotEmpty(t, id1)
	assert.NotEmpty(t, id2)
	assert.NotEqual(t, id1, id2)
	assert.Contains(t, id1, "cau-")
}

// TestInjectTraceContext 测试注入追踪上下文
func TestInjectTraceContext(t *testing.T) {
	ctx := context.Background()
	correlationID := "cor-123"
	causationID := "cau-456"

	ctx = WithCorrelationID(ctx, correlationID)
	ctx = WithCausationID(ctx, causationID)

	metadata := make(map[string]any)
	InjectTraceContext(ctx, metadata)

	assert.Equal(t, correlationID, metadata["correlation_id"])
	assert.Equal(t, causationID, metadata["causation_id"])
}

// TestInjectTraceContext_EmptyContext 测试空 context
func TestInjectTraceContext_EmptyContext(t *testing.T) {
	ctx := context.Background()
	metadata := make(map[string]any)

	InjectTraceContext(ctx, metadata)

	assert.Empty(t, metadata)
}

// TestInjectTraceContext_NilContext 测试 nil context
func TestInjectTraceContext_NilContext(t *testing.T) {
	metadata := make(map[string]any)
	InjectTraceContext(nil, metadata)

	assert.Empty(t, metadata)
}

// TestInjectTraceContext_NilMetadata 测试 nil metadata
func TestInjectTraceContext_NilMetadata(t *testing.T) {
	ctx := WithCorrelationID(context.Background(), "cor-123")
	InjectTraceContext(ctx, nil) // 不应该 panic
}

// TestExtractTraceContext 测试提取追踪上下文
func TestExtractTraceContext(t *testing.T) {
	metadata := map[string]any{
		"correlation_id": "cor-123",
		"causation_id":   "cau-456",
	}

	ctx := ExtractTraceContext(context.Background(), metadata)

	assert.Equal(t, "cor-123", GetCorrelationID(ctx))
	assert.Equal(t, "cau-456", GetCausationID(ctx))
}

// TestExtractTraceContext_EmptyMetadata 测试空 metadata
func TestExtractTraceContext_EmptyMetadata(t *testing.T) {
	metadata := make(map[string]any)
	ctx := ExtractTraceContext(context.Background(), metadata)

	assert.Empty(t, GetCorrelationID(ctx))
	assert.Empty(t, GetCausationID(ctx))
}

// TestExtractTraceContext_NilMetadata 测试 nil metadata
func TestExtractTraceContext_NilMetadata(t *testing.T) {
	ctx := ExtractTraceContext(context.Background(), nil)
	require.NotNil(t, ctx)

	assert.Empty(t, GetCorrelationID(ctx))
	assert.Empty(t, GetCausationID(ctx))
}

// TestRoundTrip 测试往返转换
func TestRoundTrip(t *testing.T) {
	// Context -> Metadata
	ctx := context.Background()
	ctx = WithCorrelationID(ctx, "cor-123")
	ctx = WithCausationID(ctx, "cau-456")

	metadata := make(map[string]any)
	InjectTraceContext(ctx, metadata)

	// Metadata -> Context
	ctx2 := ExtractTraceContext(context.Background(), metadata)

	assert.Equal(t, "cor-123", GetCorrelationID(ctx2))
	assert.Equal(t, "cau-456", GetCausationID(ctx2))
}

// BenchmarkWithCorrelationID 性能测试
func BenchmarkWithCorrelationID(b *testing.B) {
	ctx := context.Background()
	for i := 0; i < b.N; i++ {
		_ = WithCorrelationID(ctx, "cor-123")
	}
}

// BenchmarkGetCorrelationID 性能测试
func BenchmarkGetCorrelationID(b *testing.B) {
	ctx := WithCorrelationID(context.Background(), "cor-123")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = GetCorrelationID(ctx)
	}
}

// BenchmarkInjectTraceContext 性能测试
func BenchmarkInjectTraceContext(b *testing.B) {
	ctx := WithCorrelationID(context.Background(), "cor-123")
	ctx = WithCausationID(ctx, "cau-456")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		metadata := make(map[string]any)
		InjectTraceContext(ctx, metadata)
	}
}
