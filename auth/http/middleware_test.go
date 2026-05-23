package authhttp

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	auth "gochen/auth"
	"gochen/errors"
	"gochen/httpx"
)

type requestContext struct{ context.Context }

func (c requestContext) WithContext(ctx context.Context) httpx.IRequestContext {
	return requestContext{Context: ctx}
}
func (c requestContext) WithValue(key any, value any) httpx.IRequestContext {
	return requestContext{Context: context.WithValue(c.Context, key, value)}
}
func (c requestContext) WithTimeout(timeout time.Duration) (httpx.IRequestContext, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(c.Context, timeout)
	return requestContext{Context: ctx}, cancel
}
func (c requestContext) WithCancel() (httpx.IRequestContext, context.CancelFunc) {
	ctx, cancel := context.WithCancel(c.Context)
	return requestContext{Context: ctx}, cancel
}
func (c requestContext) WithDeadline(deadline time.Time) (httpx.IRequestContext, context.CancelFunc) {
	ctx, cancel := context.WithDeadline(c.Context, deadline)
	return requestContext{Context: ctx}, cancel
}
func (c requestContext) Clone() httpx.IRequestContext { return requestContext{Context: c.Context} }

type testContext struct {
	reqCtx httpx.IRequestContext
}

func (c *testContext) Method() string                        { return "GET" }
func (c *testContext) Path() string                          { return "/" }
func (c *testContext) Header(string) string                  { return "" }
func (c *testContext) Query(string) string                   { return "" }
func (c *testContext) Param(string) string                   { return "" }
func (c *testContext) QueryParams() url.Values               { return nil }
func (c *testContext) Body() ([]byte, error)                 { return nil, nil }
func (c *testContext) Request() *http.Request                { return nil }
func (c *testContext) ClientIP() string                      { return "" }
func (c *testContext) UserAgent() string                     { return "" }
func (c *testContext) BindJSON(any) error                    { return nil }
func (c *testContext) BindQuery(any) error                   { return nil }
func (c *testContext) ShouldBindJSON(any) error              { return nil }
func (c *testContext) SetStatus(int)                         {}
func (c *testContext) SetHeader(string, string)              {}
func (c *testContext) JSON(int, httpx.JSONBody) error        { return nil }
func (c *testContext) String(int, string) error              { return nil }
func (c *testContext) Data(int, string, []byte) error        { return nil }
func (c *testContext) Set(string, httpx.ContextValue)        {}
func (c *testContext) Get(string) (httpx.ContextValue, bool) { return httpx.ContextValue{}, false }
func (c *testContext) Required(string) (httpx.ContextValue, error) {
	return httpx.ContextValue{}, errors.NewCode(errors.NotFound, "missing")
}
func (c *testContext) Abort()                                  {}
func (c *testContext) AbortWithStatus(int)                     {}
func (c *testContext) AbortWithStatusJSON(int, httpx.JSONBody) {}
func (c *testContext) IsAborted() bool                         { return false }
func (c *testContext) RequestContext() httpx.IRequestContext   { return c.reqCtx }
func (c *testContext) SetContext(ctx httpx.IRequestContext)    { c.reqCtx = ctx }

func boundTestContext(t *testing.T, principal auth.Principal) *testContext {
	t.Helper()
	bound, err := auth.WithPrincipal(context.Background(), principal)
	if err != nil {
		t.Fatalf("WithPrincipal: %v", err)
	}
	return &testContext{reqCtx: requestContext{Context: bound}}
}

func TestPermissionMiddlewareRejectsUnauthenticatedRequest(t *testing.T) {
	ctx := &testContext{}
	err := PermissionMiddleware(auth.APIPermission("llm", auth.PermissionActionRead))(ctx, func() error { return nil })
	if !errors.Is(err, errors.Unauthorized) {
		t.Fatalf("expected unauthorized, got %v", err)
	}
}

func TestPermissionMiddlewareBindsHighRiskRuntime(t *testing.T) {
	ctx := boundTestContext(t, auth.Principal{
		SubjectID:   7,
		Permissions: []string{"api:llm:write"},
	})
	called := false
	err := PermissionMiddleware(
		auth.APIPermission("llm", auth.PermissionActionWrite).
			Risk(auth.PermissionRiskHigh),
	)(ctx, func() error {
		called = true
		if !auth.IsHighRiskAuthorizationFromContext(ctx.RequestContext()) {
			t.Fatal("expected high-risk runtime marker")
		}
		if _, _, err := auth.BindAuthzEvalContextOrEmpty(ctx.RequestContext()); err != nil {
			t.Fatalf("expected authz eval context binding, got %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if !called {
		t.Fatal("expected handler to be called")
	}
}

func TestPermissionMiddlewareRejectsInvalidSpec(t *testing.T) {
	ctx := boundTestContext(t, auth.Principal{
		SubjectID:   7,
		Permissions: []string{"api:llm:write"},
	})
	err := PermissionMiddleware(auth.PermissionSpec{})(ctx, func() error { return nil })
	if !errors.Is(err, errors.Internal) {
		t.Fatalf("expected internal error for invalid spec, got %v", err)
	}
}
