package httpx

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"gochen/contextx"
	"gochen/errors"
)

func TestTrustedTenantHeaderResolver_ReadsHeader(t *testing.T) {
	r, _ := http.NewRequest(http.MethodGet, "http://example.com/?tenant_id=q1", nil)
	r.Header.Set(HeaderTenantID, "h1")
	reqCtx, err := NewRequestContext(r.Context())
	if err != nil {
		t.Fatalf("NewRequestContext returned error: %v", err)
	}
	ctx := &tenantMiddlewareContext{req: r, reqCtx: reqCtx}

	got, err := TrustedTenantHeaderResolver(ctx)
	if err != nil {
		t.Fatalf("TrustedTenantHeaderResolver returned error: %v", err)
	}
	if got != "h1" {
		t.Fatalf("expected header tenant id, got %q", got)
	}
}

func TestTenantContextMiddleware_InjectsRequestContextAndSetsHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	reqCtx, err := NewRequestContext(req.Context())
	if err != nil {
		t.Fatalf("NewRequestContext returned error: %v", err)
	}
	ctx := &tenantMiddlewareContext{
		req:             req,
		reqCtx:          reqCtx,
		responseHeaders: make(http.Header),
	}

	called := false
	err = TenantContextMiddleware(func(IContext) (string, error) {
		return "t1", nil
	})(ctx, func() error {
		called = true
		if got := contextx.TenantID(ctx.RequestContext()); got != "t1" {
			t.Fatalf("expected tenant id in request context, got %q", got)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("TenantContextMiddleware returned error: %v", err)
	}
	if !called {
		t.Fatalf("expected next to be called")
	}
	if got := ctx.responseHeaders.Get(HeaderTenantID); got != "t1" {
		t.Fatalf("expected response tenant header, got %q", got)
	}
}

func TestTenantContextMiddleware_InvalidTenantHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.Header.Set(HeaderTenantID, "bad tenant")
	reqCtx, err := NewRequestContext(req.Context())
	if err != nil {
		t.Fatalf("NewRequestContext returned error: %v", err)
	}
	ctx := &tenantMiddlewareContext{req: req, reqCtx: reqCtx}

	called := false
	err = TenantContextMiddleware(TrustedTenantHeaderResolver)(ctx, func() error {
		called = true
		return nil
	})
	if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected InvalidInput, got %v", err)
	}
	if called {
		t.Fatalf("next should not be called")
	}
}

func TestTenantContextMiddleware_NilContextFailsClosed(t *testing.T) {
	called := false
	err := TenantContextMiddleware(TrustedTenantHeaderResolver)(nil, func() error {
		called = true
		return nil
	})
	if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected InvalidInput, got %v", err)
	}
	if called {
		t.Fatalf("next should not be called")
	}
}

func TestTenantContextMiddleware_NilResolverFailsClosed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	reqCtx, err := NewRequestContext(req.Context())
	if err != nil {
		t.Fatalf("NewRequestContext returned error: %v", err)
	}
	ctx := &tenantMiddlewareContext{req: req, reqCtx: reqCtx}

	called := false
	err = TenantContextMiddleware(nil)(ctx, func() error {
		called = true
		return nil
	})
	if !errors.Is(err, errors.Unauthorized) {
		t.Fatalf("expected Unauthorized, got %v", err)
	}
	if called {
		t.Fatalf("next should not be called")
	}
}

type tenantMiddlewareContext struct {
	req             *http.Request
	reqCtx          IRequestContext
	responseHeaders http.Header
}

func (c *tenantMiddlewareContext) Method() string { return c.req.Method }
func (c *tenantMiddlewareContext) Path() string   { return c.req.URL.Path }
func (c *tenantMiddlewareContext) Header(key string) string {
	return c.req.Header.Get(key)
}
func (c *tenantMiddlewareContext) Query(key string) string { return c.req.URL.Query().Get(key) }
func (c *tenantMiddlewareContext) Param(string) string     { return "" }
func (c *tenantMiddlewareContext) QueryParams() url.Values { return c.req.URL.Query() }
func (c *tenantMiddlewareContext) Body() ([]byte, error)   { return nil, nil }
func (c *tenantMiddlewareContext) ClientIP() string        { return "" }
func (c *tenantMiddlewareContext) UserAgent() string       { return "" }
func (c *tenantMiddlewareContext) BindJSON(any) error      { return nil }
func (c *tenantMiddlewareContext) BindQuery(any) error     { return nil }
func (c *tenantMiddlewareContext) SetStatus(int)           {}
func (c *tenantMiddlewareContext) SetHeader(key, value string) {
	if c.responseHeaders == nil {
		c.responseHeaders = make(http.Header)
	}
	c.responseHeaders.Set(key, value)
}
func (c *tenantMiddlewareContext) JSON(int, JSONBody) error       { return nil }
func (c *tenantMiddlewareContext) String(int, string) error       { return nil }
func (c *tenantMiddlewareContext) Data(int, string, []byte) error { return nil }
func (c *tenantMiddlewareContext) Set(string, ContextValue)       {}
func (c *tenantMiddlewareContext) Get(string) (ContextValue, bool) {
	return ContextValue{}, false
}
func (c *tenantMiddlewareContext) Required(string) (ContextValue, error) {
	return ContextValue{}, errors.NewCode(errors.NotFound, "missing")
}
func (c *tenantMiddlewareContext) Abort()                            {}
func (c *tenantMiddlewareContext) AbortWithStatus(int)               {}
func (c *tenantMiddlewareContext) AbortWithStatusJSON(int, JSONBody) {}
func (c *tenantMiddlewareContext) IsAborted() bool                   { return false }
func (c *tenantMiddlewareContext) RequestContext() IRequestContext   { return c.reqCtx }
func (c *tenantMiddlewareContext) SetContext(reqCtx IRequestContext) { c.reqCtx = reqCtx }
