package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"gochen/contextx"
)

// TestExtractTenantIDFromRequest_Priority 验证 ExtractTenantIDFromRequest Priority。
func TestExtractTenantIDFromRequest_Priority(t *testing.T) {
	r, _ := http.NewRequest(http.MethodGet, "http://example.com/?tenant_id=q1", nil)
	r.Header.Set(HeaderTenantID, "h1")

	if got := ExtractTenantIDFromRequest(r); got != "h1" {
		t.Fatalf("expected header tenant id, got %q", got)
	}
}

// TestTenantMiddleware_InjectsContextAndSetsHeader 验证 TenantMiddleware InjectsContextAndSetsHeader。
func TestTenantMiddleware_InjectsContextAndSetsHeader(t *testing.T) {
	h := TenantMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := contextx.TenantID(r.Context()); got != "t1" {
			t.Fatalf("expected tenant id in context, got %q", got)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.Header.Set(HeaderTenantID, "t1")
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if got := rr.Header().Get(HeaderTenantID); got != "t1" {
		t.Fatalf("expected response header tenant id, got %q", got)
	}
}

// TestTenantMiddleware_NoTenant_NoHeader 验证 TenantMiddleware NoTenant NoHeader。
func TestTenantMiddleware_NoTenant_NoHeader(t *testing.T) {
	h := TenantMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := contextx.TenantID(r.Context()); got != "" {
			t.Fatalf("expected empty tenant id, got %q", got)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)
	if got := rr.Header().Get(HeaderTenantID); got != "" {
		t.Fatalf("expected no response tenant header, got %q", got)
	}
}

func TestTenantMiddleware_InvalidTenantHeader_ReturnsBadRequest(t *testing.T) {
	called := false
	h := TenantMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.Header.Set(HeaderTenantID, "bad tenant")
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
	if called {
		t.Fatalf("next handler should not be called for invalid tenant header")
	}
}
