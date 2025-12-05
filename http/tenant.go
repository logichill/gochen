package http

import (
	"context"
	"net/http"
)

// Context keys for tenant
type tenantContextKey string

const (
	contextKeyTenantID tenantContextKey = "tenant_id"
)

// Header name for tenant ID
const (
	HeaderTenantID = "X-Tenant-ID"
)

// WithTenantID 在 context 中设置租户 ID
//
// Tenant ID 用于多租户场景的数据隔离。
//
// 示例:
//
//	ctx := httpx.WithTenantID(ctx, "tenant-123")
//	tenantID := httpx.GetTenantID(ctx) // "tenant-123"
func WithTenantID(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, contextKeyTenantID, tenantID)
}

// GetTenantID 从 context 中获取租户 ID
//
// 如果不存在，返回空字符串。
func GetTenantID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if id, ok := ctx.Value(contextKeyTenantID).(string); ok {
		return id
	}
	return ""
}

// ExtractTenantIDFromRequest 从 HTTP 请求中提取租户 ID
//
// 支持以下方式（按优先级）：
//  1. Header: X-Tenant-ID
//  2. Query: tenant_id
//  3. 空字符串（无租户）
//
// 示例:
//
//	tenantID := httpx.ExtractTenantIDFromRequest(r)
//	ctx := httpx.WithTenantID(r.Context(), tenantID)
func ExtractTenantIDFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	// 1. 从 Header 提取
	if tenantID := r.Header.Get(HeaderTenantID); tenantID != "" {
		return tenantID
	}

	// 2. 从 Query 提取
	if tenantID := r.URL.Query().Get("tenant_id"); tenantID != "" {
		return tenantID
	}

	// 3. 无租户
	return ""
}

// InjectTenantID 将租户 ID 注入到 metadata
//
// 从 context 中提取租户 ID，注入到提供的 metadata map 中。
//
// 示例:
//
//	metadata := make(map[string]any)
//	httpx.InjectTenantID(ctx, metadata)
//	// metadata["tenant_id"] = "tenant-123"
func InjectTenantID(ctx context.Context, metadata map[string]any) {
	if ctx == nil || metadata == nil {
		return
	}

	if tenantID := GetTenantID(ctx); tenantID != "" {
		metadata["tenant_id"] = tenantID
	}
}

// ExtractTenantIDFromMetadata 从 metadata 提取租户 ID
//
// 从 metadata map 中提取租户 ID。
//
// 示例:
//
//	metadata := map[string]any{
//	    "tenant_id": "tenant-123",
//	}
//	tenantID := httpx.ExtractTenantIDFromMetadata(metadata) // "tenant-123"
func ExtractTenantIDFromMetadata(metadata map[string]any) string {
	if metadata == nil {
		return ""
	}

	if tenantID, ok := metadata["tenant_id"].(string); ok {
		return tenantID
	}

	return ""
}

// WithTenantIDFromMetadata 从 metadata 提取租户 ID 并注入到 context
//
// 从 metadata map 中提取租户 ID，注入到返回的 context 中。
//
// 示例:
//
//	metadata := map[string]any{"tenant_id": "tenant-123"}
//	ctx := httpx.WithTenantIDFromMetadata(context.Background(), metadata)
//	tenantID := httpx.GetTenantID(ctx) // "tenant-123"
func WithTenantIDFromMetadata(ctx context.Context, metadata map[string]any) context.Context {
	if ctx == nil || metadata == nil {
		return ctx
	}

	if tenantID := ExtractTenantIDFromMetadata(metadata); tenantID != "" {
		ctx = WithTenantID(ctx, tenantID)
	}

	return ctx
}

// TenantMiddleware HTTP 中间件，自动提取租户 ID
//
// 从 HTTP 请求中提取租户 ID，注入到 Context 中。
//
// 使用示例:
//
//	mux := http.NewServeMux()
//	handler := httpx.TenantMiddleware(mux)
//	http.ListenAndServe(":8080", handler)
//
// 在 handler 中：
//
//	func MyHandler(w http.ResponseWriter, r *http.Request) {
//	    tenantID := httpx.GetTenantID(r.Context())
//	    // 使用 tenantID...
//	}
func TenantMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tenantID := ExtractTenantIDFromRequest(r)

		// 注入到 Context
		ctx := WithTenantID(r.Context(), tenantID)

		// 可选：将租户 ID 添加到响应头
		if tenantID != "" {
			w.Header().Set(HeaderTenantID, tenantID)
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
