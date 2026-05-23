package middleware

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"gochen/errors"
	"gochen/httpx"
)

type stubContext struct {
	reqCtx   httpx.IRequestContext
	aborted  bool
	status   int
	storage  map[string]httpx.ContextValue
	path     string
	clientIP string
}

func (c *stubContext) Method() string { return "GET" }
func (c *stubContext) Path() string {
	if c.path != "" {
		return c.path
	}
	return "/"
}
func (c *stubContext) Header(string) string { return "" }
func (c *stubContext) Query(string) string  { return "" }
func (c *stubContext) Param(string) string  { return "" }
func (c *stubContext) QueryParams() url.Values {
	return url.Values{}
}
func (c *stubContext) Body() ([]byte, error)  { return nil, nil }
func (c *stubContext) Request() *http.Request { return nil }
func (c *stubContext) ClientIP() string {
	if c.clientIP != "" {
		return c.clientIP
	}
	return ""
}
func (c *stubContext) UserAgent() string              { return "" }
func (c *stubContext) BindJSON(any) error             { return nil }
func (c *stubContext) BindQuery(any) error            { return nil }
func (c *stubContext) ShouldBindJSON(any) error       { return nil }
func (c *stubContext) SetStatus(code int)             { c.status = code }
func (c *stubContext) SetHeader(string, string)       {}
func (c *stubContext) JSON(int, httpx.JSONBody) error { return nil }
func (c *stubContext) String(int, string) error       { return nil }
func (c *stubContext) Data(int, string, []byte) error { return nil }

func (c *stubContext) Set(key string, value httpx.ContextValue) {
	if c.storage == nil {
		c.storage = make(map[string]httpx.ContextValue)
	}
	c.storage[key] = value
}

func (c *stubContext) Get(key string) (httpx.ContextValue, bool) {
	if c.storage == nil {
		return httpx.ContextValue{}, false
	}
	v, ok := c.storage[key]
	return v, ok
}

func (c *stubContext) Required(key string) (httpx.ContextValue, error) {
	v, ok := c.Get(key)
	if !ok {
		return httpx.ContextValue{}, errors.New("missing key")
	}
	return v, nil
}

func (c *stubContext) Abort()                   { c.aborted = true }
func (c *stubContext) AbortWithStatus(code int) { c.aborted = true; c.status = code }
func (c *stubContext) AbortWithStatusJSON(code int, _ httpx.JSONBody) {
	c.aborted = true
	c.status = code
}
func (c *stubContext) IsAborted() bool { return c.aborted }

func (c *stubContext) RequestContext() httpx.IRequestContext { return c.reqCtx }
func (c *stubContext) SetContext(ctx httpx.IRequestContext)  { c.reqCtx = ctx }

func TestTimeout_NilContext_ReturnsError(t *testing.T) {
	mw := Timeout(10 * time.Millisecond)
	if err := mw(nil, func() error { return nil }); err == nil {
		t.Fatalf("expected error, got nil")
	} else if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected invalid input error, got: %#v", err)
	}
}

func TestTimeout_NilRequestContext_ReturnsError(t *testing.T) {
	mw := Timeout(10 * time.Millisecond)
	ctx := &stubContext{reqCtx: nil}
	if err := mw(ctx, func() error { return nil }); err == nil {
		t.Fatalf("expected error, got nil")
	} else if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected invalid input error, got: %#v", err)
	}
}

func TestTimeout_StopIsBoundedOnTimeout(t *testing.T) {
	mw := Timeout(10 * time.Millisecond)
	reqCtx, err := httpx.NewRequestContext(context.Background())
	if err != nil {
		t.Fatalf("NewRequestContext returned error: %v", err)
	}
	ctx := &stubContext{reqCtx: reqCtx}

	blockCh := make(chan struct{})
	start := time.Now()
	gotErr := mw(ctx, func() error {
		<-blockCh
		return nil
	})
	elapsed := time.Since(start)
	close(blockCh)

	if gotErr == nil {
		t.Fatalf("expected error, got nil")
	}
	if !errors.Is(gotErr, errors.Timeout) {
		t.Fatalf("expected timeout error, got: %#v", gotErr)
	}
	if !errors.Is(gotErr, context.DeadlineExceeded) {
		t.Fatalf("expected errors.Is(err, context.DeadlineExceeded)=true, got: %#v", gotErr)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("expected bounded timeout stop, elapsed=%v", elapsed)
	}
}
