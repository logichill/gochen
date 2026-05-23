package nethttp

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"gochen/httpx"
)

// TestHttpServer_MiddlewareOrder 验证 HttpServer MiddlewareOrder。
func TestHttpServer_MiddlewareOrder(t *testing.T) {
	cfg := &httpx.WebConfig{}
	srv := NewServer(cfg)

	order := make([]string, 0)

	// 全局中间件
	srv.Use(func(ctx httpx.IContext, next func() error) error {
		order = append(order, "global-before")
		err := next()
		order = append(order, "global-after")
		return err
	})

	// 路由组中间件
	group := srv.Group("/api")
	group.Use(func(ctx httpx.IContext, next func() error) error {
		order = append(order, "group-before")
		err := next()
		order = append(order, "group-after")
		return err
	})

	// 终结处理器
	group.GET("/test", func(ctx httpx.IContext) error {
		order = append(order, "handler")
		return ctx.JSON(http.StatusOK, httpx.JSONValue(map[string]string{"ok": "1"}))
	})

	srv.registerRoutes()

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rec := httptest.NewRecorder()

	srv.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	expected := []string{"global-before", "group-before", "handler", "group-after", "global-after"}
	if !reflect.DeepEqual(order, expected) {
		t.Fatalf("unexpected middleware order: got %v, want %v", order, expected)
	}
}

func TestHttpServer_GroupMiddlewareInheritance(t *testing.T) {
	cfg := &httpx.WebConfig{}
	srv := NewServer(cfg)

	order := make([]string, 0)

	// 全局中间件
	srv.Use(func(ctx httpx.IContext, next func() error) error {
		order = append(order, "global-before")
		err := next()
		order = append(order, "global-after")
		return err
	})

	// base group
	base := srv.Group("/api")
	base.Use(func(ctx httpx.IContext, next func() error) error {
		order = append(order, "base-before")
		err := next()
		order = append(order, "base-after")
		return err
	})

	// child group should inherit base middlewares
	child := base.Group("")
	child.Use(func(ctx httpx.IContext, next func() error) error {
		order = append(order, "child-before")
		err := next()
		order = append(order, "child-after")
		return err
	})

	child.GET("/test", func(ctx httpx.IContext) error {
		order = append(order, "handler")
		return ctx.JSON(http.StatusOK, httpx.JSONValue(map[string]string{"ok": "1"}))
	})

	srv.registerRoutes()

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	expected := []string{
		"global-before",
		"base-before",
		"child-before",
		"handler",
		"child-after",
		"base-after",
		"global-after",
	}
	if !reflect.DeepEqual(order, expected) {
		t.Fatalf("unexpected middleware order: got %v, want %v", order, expected)
	}
}

func TestHttpServer_AutoOptions_GoesThroughMiddlewares(t *testing.T) {
	cfg := &httpx.WebConfig{}
	srv := NewServer(cfg)

	var called bool
	srv.Use(func(ctx httpx.IContext, next func() error) error {
		called = true
		ctx.SetHeader("X-Test", "1")
		return next()
	})

	srv.GET("/api/test", func(ctx httpx.IContext) error {
		return ctx.String(http.StatusOK, "ok")
	})
	srv.registerRoutes()

	req := httptest.NewRequest(http.MethodOptions, "/api/test", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)

	if !called {
		t.Fatalf("expected middleware to be called")
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, rec.Code)
	}
	if got := rec.Header().Get("X-Test"); got != "1" {
		t.Fatalf("expected X-Test=1, got %q", got)
	}
}

func TestHttpServer_AutoOptions_MergesMiddlewaresForSamePath(t *testing.T) {
	cfg := &httpx.WebConfig{}
	srv := NewServer(cfg)

	order := make([]string, 0)
	groupA := srv.Group("/api")
	groupA.Use(func(ctx httpx.IContext, next func() error) error {
		order = append(order, "group-a")
		ctx.SetHeader("X-Group-A", "1")
		return next()
	})
	groupB := srv.Group("/api")
	groupB.Use(func(ctx httpx.IContext, next func() error) error {
		order = append(order, "group-b")
		ctx.SetHeader("X-Group-B", "1")
		return next()
	})

	groupA.GET("/shared", func(ctx httpx.IContext) error {
		return ctx.String(http.StatusOK, "get")
	})
	groupB.POST("/shared", func(ctx httpx.IContext) error {
		return ctx.String(http.StatusOK, "post")
	})
	srv.registerRoutes()

	req := httptest.NewRequest(http.MethodOptions, "/api/shared", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, rec.Code)
	}
	expectedOrder := []string{"group-a", "group-b"}
	if !reflect.DeepEqual(order, expectedOrder) {
		t.Fatalf("unexpected middleware order: got %v, want %v", order, expectedOrder)
	}
	if got := rec.Header().Get("X-Group-A"); got != "1" {
		t.Fatalf("expected X-Group-A=1, got %q", got)
	}
	if got := rec.Header().Get("X-Group-B"); got != "1" {
		t.Fatalf("expected X-Group-B=1, got %q", got)
	}
	if got := rec.Header().Get("Allow"); got != "GET, POST, OPTIONS" {
		t.Fatalf("expected Allow %q, got %q", "GET, POST, OPTIONS", got)
	}
}

func TestHttpServer_AutoOptions_DeduplicatesInheritedMiddlewarePrefix(t *testing.T) {
	cfg := &httpx.WebConfig{}
	srv := NewServer(cfg)

	order := make([]string, 0)
	commonCalls := 0
	base := srv.Group("/api")
	base.Use(func(ctx httpx.IContext, next func() error) error {
		commonCalls++
		order = append(order, "common")
		return next()
	})
	getGroup := base.Group("/v1")
	getGroup.Use(func(ctx httpx.IContext, next func() error) error {
		order = append(order, "get")
		return next()
	})
	postGroup := base.Group("/v1")
	postGroup.Use(func(ctx httpx.IContext, next func() error) error {
		order = append(order, "post")
		return next()
	})

	getGroup.GET("/shared", func(ctx httpx.IContext) error {
		return ctx.String(http.StatusOK, "get")
	})
	postGroup.POST("/shared", func(ctx httpx.IContext) error {
		return ctx.String(http.StatusOK, "post")
	})
	srv.registerRoutes()

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/shared", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, rec.Code)
	}
	expectedOrder := []string{"common", "get", "post"}
	if !reflect.DeepEqual(order, expectedOrder) {
		t.Fatalf("unexpected middleware order: got %v, want %v", order, expectedOrder)
	}
	if commonCalls != 1 {
		t.Fatalf("expected inherited common middleware once, got %d", commonCalls)
	}
}

func TestHttpServer_AutoOptions_Preflight_HasCORSHeaders(t *testing.T) {
	cfg := &httpx.WebConfig{}
	srv := NewServer(cfg)

	// 不引入 httpx/middleware（避免 nethttp<-middleware<-nethttp 的循环依赖），
	// 用一个最小 CORS preflight middleware 验证 “OPTIONS 会走中间件链” 的关键语义。
	srv.Use(func(ctx httpx.IContext, next func() error) error {
		origin := ctx.Header("Origin")
		if origin != "" {
			ctx.SetHeader("Access-Control-Allow-Origin", origin)
		}
		if ctx.Method() == http.MethodOptions && origin != "" && ctx.Header("Access-Control-Request-Method") != "" {
			return ctx.String(http.StatusNoContent, "")
		}
		return next()
	})

	srv.GET("/api/test", func(ctx httpx.IContext) error {
		return ctx.String(http.StatusOK, "ok")
	})
	srv.registerRoutes()

	req := httptest.NewRequest(http.MethodOptions, "/api/test", nil)
	req.Header.Set("Origin", "https://a.example")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "Content-Type")
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://a.example" {
		t.Fatalf("expected allow origin %q, got %q", "https://a.example", got)
	}
}

func TestHttpServer_ExplicitOptions_DoesNotConflictWithAutoOptions(t *testing.T) {
	cfg := &httpx.WebConfig{}
	srv := NewServer(cfg)

	srv.GET("/api/test", func(ctx httpx.IContext) error {
		return ctx.String(http.StatusOK, "ok")
	})
	srv.OPTIONS("/api/test", func(ctx httpx.IContext) error {
		return ctx.String(http.StatusOK, "explicit")
	})

	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("registerRoutes should not panic, got: %v", rec)
		}
	}()
	srv.registerRoutes()

	req := httptest.NewRequest(http.MethodOptions, "/api/test", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
	if got := rec.Body.String(); got != "explicit" {
		t.Fatalf("expected body %q, got %q", "explicit", got)
	}
}

// TestHttpServer_PathParams 验证 HttpServer PathParams。
func TestHttpServer_PathParams(t *testing.T) {
	cfg := &httpx.WebConfig{}
	srv := NewServer(cfg)

	var gotID string

	srv.GET("/users/:id", func(ctx httpx.IContext) error {
		gotID = ctx.Param("id")
		return ctx.String(http.StatusOK, "ok")
	})

	srv.registerRoutes()

	req := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	rec := httptest.NewRecorder()

	srv.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
	if gotID != "42" {
		t.Fatalf("expected id=42, got %q", gotID)
	}
}

func TestHttpServer_ApplyTimeoutDefaults_IncludesHeaderGuards(t *testing.T) {
	cfg := &httpx.WebConfig{}
	srv := NewServer(cfg)

	srv.applyTimeoutDefaults()

	if cfg.ReadHeaderTimeout != httpx.DefaultReadHeaderTimeout {
		t.Fatalf("expected ReadHeaderTimeout=%v, got %v", httpx.DefaultReadHeaderTimeout, cfg.ReadHeaderTimeout)
	}
	if cfg.MaxHeaderBytes != httpx.DefaultMaxHeaderBytes {
		t.Fatalf("expected MaxHeaderBytes=%v, got %v", httpx.DefaultMaxHeaderBytes, cfg.MaxHeaderBytes)
	}
}

func TestHttpServer_TLSMinVersion_DefaultsTo12(t *testing.T) {
	cfg := &httpx.WebConfig{TLSEnabled: true}
	srv := NewServer(cfg)

	if err := srv.applySecurityDefaults(); err != nil {
		t.Fatalf("applySecurityDefaults returned error: %v", err)
	}
	httpSrv, err := srv.newHTTPServer(":0")
	if err != nil {
		t.Fatalf("newHTTPServer returned error: %v", err)
	}
	if httpSrv.TLSConfig == nil {
		t.Fatalf("expected TLSConfig not nil")
	}
	if httpSrv.TLSConfig.MinVersion != tls.VersionTLS12 {
		t.Fatalf("expected MinVersion=%v, got %v", tls.VersionTLS12, httpSrv.TLSConfig.MinVersion)
	}
}

func TestHttpServer_TLSMinVersion_CanBeSetTo13(t *testing.T) {
	cfg := &httpx.WebConfig{TLSEnabled: true, TLSMinVersion: "1.3"}
	srv := NewServer(cfg)

	if err := srv.applySecurityDefaults(); err != nil {
		t.Fatalf("applySecurityDefaults returned error: %v", err)
	}
	httpSrv, err := srv.newHTTPServer(":0")
	if err != nil {
		t.Fatalf("newHTTPServer returned error: %v", err)
	}
	if httpSrv.TLSConfig == nil {
		t.Fatalf("expected TLSConfig not nil")
	}
	if httpSrv.TLSConfig.MinVersion != tls.VersionTLS13 {
		t.Fatalf("expected MinVersion=%v, got %v", tls.VersionTLS13, httpSrv.TLSConfig.MinVersion)
	}
}

func TestHttpServer_TLSMinVersion_Rejects11(t *testing.T) {
	cfg := &httpx.WebConfig{TLSEnabled: true, TLSMinVersion: "1.1"}
	srv := NewServer(cfg)

	if err := srv.applySecurityDefaults(); err == nil {
		t.Fatalf("expected error")
	}
}

func TestHttpServer_TLSMinVersion_TLSConfigCannotBeLower(t *testing.T) {
	cfg := &httpx.WebConfig{
		TLSEnabled:    true,
		TLSMinVersion: "1.3",
		TLSConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
	}
	srv := NewServer(cfg)

	if err := srv.applySecurityDefaults(); err != nil {
		t.Fatalf("applySecurityDefaults returned error: %v", err)
	}
	if _, err := srv.newHTTPServer(":0"); err == nil {
		t.Fatalf("expected error")
	}
}

func TestHttpServer_Start_TLSRequiresCertificateSource(t *testing.T) {
	cfg := &httpx.WebConfig{TLSEnabled: true}
	srv := NewServer(cfg)

	if err := srv.Start(":0"); err == nil {
		t.Fatalf("expected error")
	}
}

func writeFile(t *testing.T, name string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
}

func TestHttpServer_Static_SafeDefaults_BlockHiddenAndDirList(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".env"), "secret")
	writeFile(t, filepath.Join(root, "dir", "a.txt"), "ok")

	cfg := &httpx.WebConfig{}
	srv := NewServer(cfg)
	srv.Static("/static/", root)

	{
		req := httptest.NewRequest(http.MethodGet, "/static/.env", nil)
		rec := httptest.NewRecorder()
		srv.mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected status %d, got %d", http.StatusNotFound, rec.Code)
		}
	}
	{
		req := httptest.NewRequest(http.MethodGet, "/static/dir/", nil)
		rec := httptest.NewRecorder()
		srv.mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected status %d, got %d", http.StatusNotFound, rec.Code)
		}
	}
}

func TestHttpServer_Static_SafeDefaults_SetsCacheControl(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "app.js"), "console.log('ok')")
	writeFile(t, filepath.Join(root, "index.html"), "<html>ok</html>")

	cfg := &httpx.WebConfig{}
	srv := NewServer(cfg)
	srv.Static("/static/", root)

	{
		req := httptest.NewRequest(http.MethodGet, "/static/app.js", nil)
		rec := httptest.NewRecorder()
		srv.mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
		}
		if got := rec.Header().Get("Cache-Control"); got != "public, max-age=3600" {
			t.Fatalf("expected Cache-Control public, max-age=3600, got %q", got)
		}
		if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Fatalf("expected X-Content-Type-Options nosniff, got %q", got)
		}
	}
	{
		req := httptest.NewRequest(http.MethodGet, "/static/", nil)
		rec := httptest.NewRecorder()
		srv.mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
			t.Fatalf("expected Cache-Control no-cache, got %q", got)
		}
	}
}

func TestHttpServer_Static_SafeDefaults_BlockSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink behavior differs on windows")
	}

	root := t.TempDir()
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "secret.txt")
	writeFile(t, outsideFile, "top-secret")

	if err := os.Symlink(outsideFile, filepath.Join(root, "leak.txt")); err != nil {
		t.Fatalf("Symlink returned error: %v", err)
	}

	cfg := &httpx.WebConfig{}
	srv := NewServer(cfg)
	srv.Static("/static/", root)

	req := httptest.NewRequest(http.MethodGet, "/static/leak.txt", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, rec.Code)
	}
}
