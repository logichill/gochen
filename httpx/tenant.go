package httpx

import (
	"net/http"
	"strings"

	"gochen/contextx"
)

const (
	// HeaderTenantID 表示租户 ID 的 HTTP Header 名称。
	HeaderTenantID = "X-Tenant-ID"
)

// ExtractTenantIDFromRequest 从 HTTP 请求中提取租户 ID。
//
// 说明：
// - 支持以下方式（按优先级）：
// - 1. Header: X-Tenant-ID
// - 2. 空字符串（无租户）
// - 示例:
// - tenantID := httpx.ExtractTenantIDFromRequest(r)
// - ctx, err := contextx.WithTenantID(r.Context(), tenantID)
func ExtractTenantIDFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	// 1. 从 Header 提取
	if tenantID := SanitizeIdentifierFromHeader(r.Header.Get(HeaderTenantID), 128); tenantID != "" {
		return tenantID
	}

	// 2. 无租户
	return ""
}

// TenantMiddleware HTTP 中间件，自动提取租户 ID。
//
// 说明：
// - 从 HTTP 请求中提取租户 ID，注入到 Context 中。
// - 使用示例:
// - mux := http.NewServeMux()
// - handler := httpx.TenantMiddleware(mux)
// - http.ListenAndServe(":8080", handler)
// - 在 handler 中：
// - func MyHandler(w http.ResponseWriter, r *http.Request) {
// - tenantID := contextx.TenantID(r.Context())
// - // 使用 tenantID...
// - }
func TenantMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawTenantID := r.Header.Get(HeaderTenantID)
		tenantID := ExtractTenantIDFromRequest(r)
		if strings.TrimSpace(rawTenantID) != "" && tenantID == "" {
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}

		// 注入到 Context
		ctx, err := contextx.WithTenantID(r.Context(), tenantID)
		if err != nil {
			// 理论上 r.Context() 不应为 nil；此处兜底防御，避免 panic。
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		// 可选：将租户 ID 添加到响应头
		if tenantID != "" {
			w.Header().Set(HeaderTenantID, tenantID)
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
