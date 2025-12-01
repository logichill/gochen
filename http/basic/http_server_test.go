package basic

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	httpx "gochen/http"
)

// TestHttpServer_MiddlewareOrder 验证全局中间件与路由组中间件的执行顺序。
func TestHttpServer_MiddlewareOrder(t *testing.T) {
	cfg := &httpx.WebConfig{}
	srv := NewHTTPServer(cfg)

	order := make([]string, 0)

	// 全局中间件
	srv.Use(func(ctx httpx.IHttpContext, next func() error) error {
		order = append(order, "global-before")
		err := next()
		order = append(order, "global-after")
		return err
	})

	// 路由组中间件
	group := srv.Group("/api")
	group.Use(func(ctx httpx.IHttpContext, next func() error) error {
		order = append(order, "group-before")
		err := next()
		order = append(order, "group-after")
		return err
	})

	// 终结处理器
	group.GET("/test", func(ctx httpx.IHttpContext) error {
		order = append(order, "handler")
		return ctx.JSON(http.StatusOK, map[string]string{"ok": "1"})
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

// TestHttpServer_PathParams 验证 :id 风格路径参数能被正确解析到 IHttpContext 中。
func TestHttpServer_PathParams(t *testing.T) {
	cfg := &httpx.WebConfig{}
	srv := NewHTTPServer(cfg)

	var gotID string

	srv.GET("/users/:id", func(ctx httpx.IHttpContext) error {
		gotID = ctx.GetParam("id")
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

