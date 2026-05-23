package httpx

import (
	"net/http"
	"net/url"
	"testing"

	"gochen/errors"
)

type respondStubContext struct {
	status int
	json   any
	text   string
}

func (c *respondStubContext) SetStatus(code int)          { c.status = code }
func (c *respondStubContext) SetHeader(key, value string) {}
func (c *respondStubContext) JSON(code int, obj JSONBody) error {
	c.status = code
	c.json, _ = JSONBodyAs[any](obj)
	return nil
}
func (c *respondStubContext) String(code int, text string) error {
	c.status = code
	c.text = text
	return nil
}
func (c *respondStubContext) Data(code int, contentType string, data []byte) error {
	c.status = code
	return nil
}
func (c *respondStubContext) Set(key string, value ContextValue)  {}
func (c *respondStubContext) Get(key string) (ContextValue, bool) { return ContextValue{}, false }
func (c *respondStubContext) Required(key string) (ContextValue, error) {
	return ContextValue{}, errors.New("missing")
}
func (c *respondStubContext) Abort()                   {}
func (c *respondStubContext) AbortWithStatus(code int) { c.status = code }
func (c *respondStubContext) AbortWithStatusJSON(code int, jsonObj JSONBody) {
	c.status = code
	c.json, _ = JSONBodyAs[any](jsonObj)
}
func (c *respondStubContext) IsAborted() bool                 { return false }
func (c *respondStubContext) RequestContext() IRequestContext { return nil }
func (c *respondStubContext) SetContext(ctx IRequestContext)  {}
func (c *respondStubContext) Method() string                  { return "" }
func (c *respondStubContext) Path() string                    { return "" }
func (c *respondStubContext) Query(key string) string         { return "" }
func (c *respondStubContext) Param(key string) string         { return "" }
func (c *respondStubContext) Header(key string) string        { return "" }
func (c *respondStubContext) QueryParams() url.Values         { return url.Values{} }
func (c *respondStubContext) Body() ([]byte, error)           { return nil, nil }
func (c *respondStubContext) Request() *http.Request          { return nil }
func (c *respondStubContext) ClientIP() string                { return "" }
func (c *respondStubContext) UserAgent() string               { return "" }
func (c *respondStubContext) BindJSON(any) error              { return nil }
func (c *respondStubContext) ShouldBindJSON(any) error        { return nil }
func (c *respondStubContext) BindQuery(any) error             { return nil }

func TestWriteSuccessStatus_WrapsPayload(t *testing.T) {
	ctx := &respondStubContext{}
	if err := WriteCreated(ctx, map[string]any{"id": 1}); err != nil {
		t.Fatalf("WriteCreated returned error: %v", err)
	}
	if ctx.status != 201 {
		t.Fatalf("expected 201, got %d", ctx.status)
	}
	payload, ok := ctx.json.(*ResponseMessage)
	if !ok {
		t.Fatalf("expected ResponseMessage, got %T", ctx.json)
	}
	if payload.Code != "ok" || payload.Data == nil {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestWriteError_UsesErrorxPayload(t *testing.T) {
	ctx := &respondStubContext{}
	if err := WriteError(ctx, errors.NewCode(errors.TooManyRequests, "rate limited")); err != nil {
		t.Fatalf("WriteError returned error: %v", err)
	}
	if ctx.status != 429 {
		t.Fatalf("expected 429, got %d", ctx.status)
	}
	payload, ok := ctx.json.(*ResponseMessage)
	if !ok {
		t.Fatalf("expected ResponseMessage, got %T", ctx.json)
	}
	if payload.Code != string(errors.TooManyRequests) || payload.Message != "rate limited" {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestWriteErrorExtra_IncludesExtraPayload(t *testing.T) {
	ctx := &respondStubContext{}
	err := WriteErrorExtra(ctx, errors.NewCode(errors.InvalidInput, "bad request"), map[string]any{"result": map[string]any{"ok": false}})
	if err != nil {
		t.Fatalf("WriteErrorExtra returned error: %v", err)
	}
	payload, ok := ctx.json.(*ResponseMessage)
	if !ok {
		t.Fatalf("expected ResponseMessage, got %T", ctx.json)
	}
	if payload.Extra == nil || payload.Extra["result"] == nil {
		t.Fatalf("expected extra payload, got %#v", payload)
	}
}

func TestWriteRedirectJSON(t *testing.T) {
	ctx := &respondStubContext{}
	if err := WriteRedirectJSON(ctx, 302, "/target", map[string]any{"redirect_url": "/target"}); err != nil {
		t.Fatalf("WriteRedirectJSON returned error: %v", err)
	}
	if ctx.status != 302 {
		t.Fatalf("expected 302, got %d", ctx.status)
	}
	payload, ok := ctx.json.(*ResponseMessage)
	if !ok {
		t.Fatalf("expected ResponseMessage, got %T", ctx.json)
	}
	if payload.Code != "redirect" {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestWriteErrorExtra_DoesNotLeakOn5xx(t *testing.T) {
	ctx := &respondStubContext{}
	err := WriteErrorExtra(ctx, errors.NewCode(errors.Internal, "boom"), map[string]any{"result": map[string]any{"ok": false}})
	if err != nil {
		t.Fatalf("WriteErrorExtra returned error: %v", err)
	}
	payload, ok := ctx.json.(*ResponseMessage)
	if !ok {
		t.Fatalf("expected ResponseMessage, got %T", ctx.json)
	}
	if payload.Extra != nil {
		t.Fatalf("expected 5xx to omit extra, got %#v", payload)
	}
}

func TestWriteSuccessStatus_RejectsNoBodyStatuses(t *testing.T) {
	ctx := &respondStubContext{}
	if err := WriteSuccessStatus(ctx, http.StatusNoContent, map[string]any{"id": 1}); err == nil {
		t.Fatalf("expected WriteSuccessStatus to reject 204")
	}
}

func TestWriteRedirectJSON_RejectsNotModified(t *testing.T) {
	ctx := &respondStubContext{}
	if err := WriteRedirectJSON(ctx, http.StatusNotModified, "/target", map[string]any{"redirect_url": "/target"}); err == nil {
		t.Fatalf("expected WriteRedirectJSON to reject 304")
	}
}
