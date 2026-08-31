package testkit

import (
	"context"
	"testing"

	"gochen/httpx"
)

func TestRecordingRouterCapturesGroupsAndMiddleware(t *testing.T) {
	router := NewRecordingRouter()
	middleware := func(httpx.IContext, func() error) error { return nil }
	group := router.Group("/api").Use(middleware).Group("v1")
	group.GET("/users", nil)
	router.POST("health", nil)

	routes := router.RecordedRoutes()
	if len(routes) != 2 {
		t.Fatalf("route count = %d; want 2", len(routes))
	}
	if routes[0].Method != "GET" || routes[0].Path != "/api/v1/users" || routes[0].MiddlewareCount != 1 {
		t.Fatalf("first route = %#v", routes[0])
	}
	if routes[1].Method != "POST" || routes[1].Path != "/health" || routes[1].MiddlewareCount != 0 {
		t.Fatalf("second route = %#v", routes[1])
	}
}

func TestRecordingServerLifecycle(t *testing.T) {
	server := NewRecordingServer()
	server.Static("/assets", "./assets")
	if err := server.Start(":8080"); err != nil {
		t.Fatalf("start: %v", err)
	}
	if !server.IsStarted() || server.Address() != ":8080" {
		t.Fatalf("started = %t, address = %q", server.IsStarted(), server.Address())
	}
	if err := server.Stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if server.IsStarted() {
		t.Fatal("server remains started after Stop")
	}
}
