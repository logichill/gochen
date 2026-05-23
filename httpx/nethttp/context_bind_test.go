package nethttp

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"gochen/errors"
	"gochen/httpx"
)

// TestHttpContext_ClientIP_SafeDefaultIgnoresProxyHeaders 验证 HttpContext ClientIP SafeDefaultIgnoresProxyHeaders。
func TestHttpContext_ClientIP_SafeDefaultIgnoresProxyHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.RemoteAddr = "1.2.3.4:12345"
	req.Header.Set("X-Forwarded-For", "9.9.9.9")
	req.Header.Set("X-Real-IP", "8.8.8.8")

	rec := httptest.NewRecorder()
	ctx, err := NewBaseContext(rec, req)
	if err != nil {
		t.Fatalf("NewBaseContext returned error: %v", err)
	}

	if got := ctx.ClientIP(); got != "1.2.3.4" {
		t.Fatalf("expected remote ip %q, got %q", "1.2.3.4", got)
	}
}

// TestHttpContext_ClientIP_TrustedProxyUsesForwardedHeaders 验证 HttpContext ClientIP TrustedProxyUsesForwardedHeaders。
func TestHttpContext_ClientIP_TrustedProxyUsesForwardedHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.RemoteAddr = "1.2.3.4:12345"
	req.Header.Set("X-Forwarded-For", "9.9.9.9, 10.0.0.1")

	rec := httptest.NewRecorder()
	ctx, err := NewBaseContext(rec, req)
	if err != nil {
		t.Fatalf("NewBaseContext returned error: %v", err)
	}
	ctx.trustedProxyChecker = func(addr netip.Addr) bool { return addr.String() == "1.2.3.4" }

	if got := ctx.ClientIP(); got != "9.9.9.9" {
		t.Fatalf("expected forwarded ip %q, got %q", "9.9.9.9", got)
	}
}

// TestHttpContext_BindQuery 验证 HttpContext BindQuery。
func TestHttpContext_BindQuery(t *testing.T) {
	type Query struct {
		Name   string   `query:"name"`
		Age    int      `query:"age"`
		Tags   []string `query:"tag"`
		Active bool     `query:"active"`
	}

	req := httptest.NewRequest(http.MethodGet, "http://example.com/?name=alice&age=30&tag=a&tag=b&active=true", nil)
	rec := httptest.NewRecorder()
	ctx, err := NewBaseContext(rec, req)
	if err != nil {
		t.Fatalf("NewBaseContext returned error: %v", err)
	}

	var q Query
	if err := ctx.BindQuery(&q); err != nil {
		t.Fatalf("BindQuery returned error: %v", err)
	}

	if q.Name != "alice" || q.Age != 30 || !q.Active || len(q.Tags) != 2 || q.Tags[0] != "a" || q.Tags[1] != "b" {
		t.Fatalf("unexpected bound query: %+v", q)
	}
}

func TestHttpContext_BindQuery_EmbeddedAndTaggedNames(t *testing.T) {
	type Filter struct {
		Status string `json:"status"`
		Limit  int    `json:"limit"`
	}
	type Query struct {
		Filter
		Page int `json:"page"`
	}

	// primary keys (tag names)
	{
		req := httptest.NewRequest(http.MethodGet, "http://example.com/?status=pending&limit=10&page=2", nil)
		rec := httptest.NewRecorder()
		ctx, err := NewBaseContext(rec, req)
		if err != nil {
			t.Fatalf("NewBaseContext returned error: %v", err)
		}

		var q Query
		if err := ctx.BindQuery(&q); err != nil {
			t.Fatalf("BindQuery returned error: %v", err)
		}
		if q.Status != "pending" || q.Limit != 10 || q.Page != 2 {
			t.Fatalf("unexpected bound query: %+v", q)
		}
	}

	// alias keys (field names)
	{
		req := httptest.NewRequest(http.MethodGet, "http://example.com/?Status=ok&Limit=3&Page=4", nil)
		rec := httptest.NewRecorder()
		ctx, err := NewBaseContext(rec, req)
		if err != nil {
			t.Fatalf("NewBaseContext returned error: %v", err)
		}

		var q Query
		if err := ctx.BindQuery(&q); err != nil {
			t.Fatalf("BindQuery returned error: %v", err)
		}
		if q.Status != "ok" || q.Limit != 3 || q.Page != 4 {
			t.Fatalf("unexpected bound query: %+v", q)
		}
	}
}

func TestHttpContext_BindQuery_TaggedNestedStructAcceptsFieldNameAliases(t *testing.T) {
	type Pagination struct {
		Page int `json:"page"`
		Size int `json:"size"`
	}
	type Query struct {
		Pagination Pagination `json:"pagination"`
	}

	req := httptest.NewRequest(
		http.MethodGet,
		"http://example.com/?Pagination.Page=2&Pagination.Size=50",
		nil,
	)
	rec := httptest.NewRecorder()
	ctx, err := NewBaseContext(rec, req)
	if err != nil {
		t.Fatalf("NewBaseContext returned error: %v", err)
	}

	var q Query
	if err := ctx.BindQuery(&q); err != nil {
		t.Fatalf("BindQuery returned error: %v", err)
	}
	if q.Pagination.Page != 2 || q.Pagination.Size != 50 {
		t.Fatalf("unexpected bound query: %+v", q)
	}
}

func TestHttpContext_BindQuery_NestedDotAndBracketAndPointerStruct(t *testing.T) {
	type Range struct {
		Start time.Time `json:"start"`
		End   time.Time `json:"end"`
	}
	type Query struct {
		Range *Range `json:"range"`
	}

	req := httptest.NewRequest(http.MethodGet,
		"http://example.com/?range.start=2026-02-01T00:00:00Z&range[end]=2026-02-02T00:00:00Z",
		nil,
	)
	rec := httptest.NewRecorder()
	ctx, err := NewBaseContext(rec, req)
	if err != nil {
		t.Fatalf("NewBaseContext returned error: %v", err)
	}

	var q Query
	if err := ctx.BindQuery(&q); err != nil {
		t.Fatalf("BindQuery returned error: %v", err)
	}
	if q.Range == nil {
		t.Fatalf("expected range to be allocated")
	}
	if q.Range.Start.UTC().Format(time.RFC3339) != "2026-02-01T00:00:00Z" {
		t.Fatalf("unexpected start: %s", q.Range.Start.UTC().Format(time.RFC3339))
	}
	if q.Range.End.UTC().Format(time.RFC3339) != "2026-02-02T00:00:00Z" {
		t.Fatalf("unexpected end: %s", q.Range.End.UTC().Format(time.RFC3339))
	}
}

func TestHttpContext_BindQuery_Duration(t *testing.T) {
	type Query struct {
		Timeout time.Duration `query:"timeout"`
	}

	req := httptest.NewRequest(http.MethodGet, "http://example.com/?timeout=1s", nil)
	rec := httptest.NewRecorder()
	ctx, err := NewBaseContext(rec, req)
	if err != nil {
		t.Fatalf("NewBaseContext returned error: %v", err)
	}

	var q Query
	if err := ctx.BindQuery(&q); err != nil {
		t.Fatalf("BindQuery returned error: %v", err)
	}
	if q.Timeout != time.Second {
		t.Fatalf("expected 1s, got %s", q.Timeout)
	}
}

// TestHttpContext_BindQuery_InvalidInt 验证 HttpContext BindQuery InvalidInt。
func TestHttpContext_BindQuery_InvalidInt(t *testing.T) {
	type Query struct {
		Age int `query:"age"`
	}

	req := httptest.NewRequest(http.MethodGet, "http://example.com/?age=not-an-int", nil)
	rec := httptest.NewRecorder()
	ctx, err := NewBaseContext(rec, req)
	if err != nil {
		t.Fatalf("NewBaseContext returned error: %v", err)
	}

	var q Query
	err = ctx.BindQuery(&q)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected invalid input error, got: %v", err)
	}
}

func TestHttpContext_BindJSON_StrictUnknownFields(t *testing.T) {
	type Payload struct {
		A int `json:"a"`
	}

	req := httptest.NewRequest(http.MethodPost, "http://example.com/", bytes.NewBufferString(`{"a":1,"b":2}`))
	rec := httptest.NewRecorder()
	ctx, err := NewBaseContext(rec, req)
	if err != nil {
		t.Fatalf("NewBaseContext returned error: %v", err)
	}

	var p Payload
	err = ctx.BindJSON(&p)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected invalid input error, got: %v", err)
	}
}

func TestHttpContext_BindJSON_StrictTrailingData(t *testing.T) {
	type Payload struct {
		A int `json:"a"`
	}

	req := httptest.NewRequest(http.MethodPost, "http://example.com/", bytes.NewBufferString(`{"a":1}{"a":2}`))
	rec := httptest.NewRecorder()
	ctx, err := NewBaseContext(rec, req)
	if err != nil {
		t.Fatalf("NewBaseContext returned error: %v", err)
	}

	var p Payload
	err = ctx.BindJSON(&p)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected invalid input error, got: %v", err)
	}
}

func TestHttpContext_BindJSON_StrictOK(t *testing.T) {
	type Payload struct {
		A int `json:"a"`
	}

	req := httptest.NewRequest(http.MethodPost, "http://example.com/", bytes.NewBufferString(`{"a":1}`))
	rec := httptest.NewRecorder()
	ctx, err := NewBaseContext(rec, req)
	if err != nil {
		t.Fatalf("NewBaseContext returned error: %v", err)
	}

	var p Payload
	if err := ctx.BindJSON(&p); err != nil {
		t.Fatalf("BindJSON returned error: %v", err)
	}
	if p.A != 1 {
		t.Fatalf("expected a=1, got %d", p.A)
	}
}

func TestHttpContext_Body_DefaultLimitAndOptOut(t *testing.T) {
	oversize := bytes.Repeat([]byte("a"), int(httpx.DefaultMaxBodySizeBytes)+1)

	// 未显式设置 MaxBodySizeKey 时，使用默认上限（10MB）兜底。
	{
		req := httptest.NewRequest(http.MethodPost, "http://example.com/", bytes.NewReader(oversize))
		rec := httptest.NewRecorder()
		ctx, err := NewBaseContext(rec, req)
		if err != nil {
			t.Fatalf("NewBaseContext returned error: %v", err)
		}

		_, err = ctx.Body()
		if err == nil {
			t.Fatalf("expected error")
		}
		if !errors.Is(err, errors.PayloadTooLarge) {
			t.Fatalf("expected payload too large error, got: %v", err)
		}
	}

	// 显式 opt-out：设置 MaxBodySizeKey=0 关闭限制。
	{
		req := httptest.NewRequest(http.MethodPost, "http://example.com/", bytes.NewReader(oversize))
		rec := httptest.NewRecorder()
		ctx, err := NewBaseContext(rec, req)
		if err != nil {
			t.Fatalf("NewBaseContext returned error: %v", err)
		}
		ctx.Set(httpx.MaxBodySizeKey, httpx.ValueOf(int64(0)))

		data, err := ctx.Body()
		if err != nil {
			t.Fatalf("Body returned error: %v", err)
		}
		if len(data) != len(oversize) {
			t.Fatalf("expected len=%d, got %d", len(oversize), len(data))
		}
	}
}
