package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"gochen/httpx"
	"gochen/httpx/nethttp"
)

func newNetHTTPContext(t *testing.T, method string, origin string) (*nethttp.Context, *httptest.ResponseRecorder) {
	t.Helper()
	req := httptest.NewRequest(method, "http://example.com/", nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	rec := httptest.NewRecorder()
	ctx, err := nethttp.NewBaseContext(rec, req)
	if err != nil {
		t.Fatalf("NewBaseContext returned error: %v", err)
	}
	return ctx, rec
}

func TestCORSFromWebConfig_SafeDefaults(t *testing.T) {
	origin := "https://a.example"

	cases := []struct {
		name string
		cfg  *httpx.WebConfig
	}{
		{name: "nil-config", cfg: nil},
		{name: "disabled", cfg: &httpx.WebConfig{CORSEnabled: false}},
		{name: "enabled-but-empty-allowlist", cfg: &httpx.WebConfig{CORSEnabled: true}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, rec := newNetHTTPContext(t, http.MethodGet, origin)
			mw := CORSFromWebConfig(tc.cfg)

			called := false
			if err := mw(ctx, func() error {
				called = true
				return ctx.String(http.StatusOK, "ok")
			}); err != nil {
				t.Fatalf("middleware returned error: %v", err)
			}

			if !called {
				t.Fatalf("expected next to be called")
			}
			if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
				t.Fatalf("expected no CORS header, got %q", got)
			}
		})
	}
}

func TestCORSFromWebConfig_Allowlist(t *testing.T) {
	origin := "https://a.example"
	cfg := &httpx.WebConfig{
		CORSEnabled:      true,
		CORSAllowOrigins: []string{origin},
		CORSAllowMethods: []string{"GET"},
		CORSAllowHeaders: []string{"Content-Type"},
	}

	ctx, rec := newNetHTTPContext(t, http.MethodGet, origin)
	mw := CORSFromWebConfig(cfg)

	if err := mw(ctx, func() error {
		return ctx.String(http.StatusOK, "ok")
	}); err != nil {
		t.Fatalf("middleware returned error: %v", err)
	}

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != origin {
		t.Fatalf("expected allow origin %q, got %q", origin, got)
	}
	if got := rec.Header().Get("Vary"); got != "Origin" {
		t.Fatalf("expected Vary=Origin, got %q", got)
	}
}

func TestCORS_NilIsNoop(t *testing.T) {
	origin := "https://a.example"
	ctx, rec := newNetHTTPContext(t, http.MethodGet, origin)

	called := false
	if err := CORS(nil)(ctx, func() error {
		called = true
		return ctx.String(http.StatusOK, "ok")
	}); err != nil {
		t.Fatalf("middleware returned error: %v", err)
	}
	if !called {
		t.Fatalf("expected next to be called")
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected no CORS header, got %q", got)
	}
}

func TestCORS_AllowAllOriginsByStar(t *testing.T) {
	origin := "https://evil.example"
	ctx, rec := newNetHTTPContext(t, http.MethodGet, origin)

	mw := CORS(&CORSConfig{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET"},
		AllowHeaders:     []string{"Content-Type"},
		AllowCredentials: false,
		MaxAge:           60,
	})

	if err := mw(ctx, func() error {
		return ctx.String(http.StatusOK, "ok")
	}); err != nil {
		t.Fatalf("middleware returned error: %v", err)
	}

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("expected allow origin '*', got %q", got)
	}
}

func TestCORS_Preflight_WritesNoContentAndSkipsNext(t *testing.T) {
	origin := "https://a.example"
	ctx, rec := newNetHTTPContext(t, http.MethodOptions, origin)
	ctx.Request().Header.Set("Access-Control-Request-Method", "POST")
	ctx.Request().Header.Set("Access-Control-Request-Headers", "Content-Type")

	mw := CORS(&CORSConfig{
		AllowOrigins: []string{origin},
		AllowMethods: []string{"GET", "POST"},
		AllowHeaders: []string{"Content-Type"},
		MaxAge:       60,
	})

	called := false
	if err := mw(ctx, func() error {
		called = true
		return ctx.String(http.StatusOK, "ok")
	}); err != nil {
		t.Fatalf("middleware returned error: %v", err)
	}

	if called {
		t.Fatalf("expected next not to be called")
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != origin {
		t.Fatalf("expected allow origin %q, got %q", origin, got)
	}
}

func TestCORS_OptionsWithoutPreflight_DoesNotShortCircuit(t *testing.T) {
	origin := "https://a.example"
	ctx, rec := newNetHTTPContext(t, http.MethodOptions, origin)

	mw := CORS(&CORSConfig{
		AllowOrigins: []string{origin},
		AllowMethods: []string{"GET", "POST"},
		AllowHeaders: []string{"Content-Type"},
		MaxAge:       60,
	})

	called := false
	if err := mw(ctx, func() error {
		called = true
		return ctx.String(http.StatusOK, "ok")
	}); err != nil {
		t.Fatalf("middleware returned error: %v", err)
	}

	if !called {
		t.Fatalf("expected next to be called")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}
