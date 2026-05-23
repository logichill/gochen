package httpx

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gochen/errors"
)

// mockRouteGroup 模拟路由组
type mockRouteGroup struct {
	routes map[string]map[string]Handler // method -> path -> handler
}

func newMockRouteGroup() *mockRouteGroup {
	return &mockRouteGroup{
		routes: make(map[string]map[string]Handler),
	}
}

func (g *mockRouteGroup) registerRoute(method, path string, handler Handler) {
	if g.routes[method] == nil {
		g.routes[method] = make(map[string]Handler)
	}
	g.routes[method][path] = handler
}

func (g *mockRouteGroup) GET(path string, handler Handler) IRouteGroup {
	g.registerRoute("GET", path, handler)
	return g
}

func (g *mockRouteGroup) POST(path string, handler Handler) IRouteGroup {
	g.registerRoute("POST", path, handler)
	return g
}

func (g *mockRouteGroup) PUT(path string, handler Handler) IRouteGroup {
	g.registerRoute("PUT", path, handler)
	return g
}

func (g *mockRouteGroup) DELETE(path string, handler Handler) IRouteGroup {
	g.registerRoute("DELETE", path, handler)
	return g
}

func (g *mockRouteGroup) PATCH(path string, handler Handler) IRouteGroup {
	g.registerRoute("PATCH", path, handler)
	return g
}

func (g *mockRouteGroup) HEAD(path string, handler Handler) IRouteGroup {
	g.registerRoute("HEAD", path, handler)
	return g
}

func (g *mockRouteGroup) OPTIONS(path string, handler Handler) IRouteGroup {
	g.registerRoute("OPTIONS", path, handler)
	return g
}

func (g *mockRouteGroup) Group(prefix string) IRouteGroup {
	return g
}

func (g *mockRouteGroup) Use(middleware ...Middleware) IRouteGroup {
	return g
}

type stubContext struct {
	aborted bool
	status  int
	storage map[string]ContextValue
}

func (c *stubContext) Method() string                 { return "GET" }
func (c *stubContext) Path() string                   { return "/" }
func (c *stubContext) Header(string) string           { return "" }
func (c *stubContext) Query(string) string            { return "" }
func (c *stubContext) Param(string) string            { return "" }
func (c *stubContext) QueryParams() url.Values        { return url.Values{} }
func (c *stubContext) Body() ([]byte, error)          { return nil, nil }
func (c *stubContext) Request() *http.Request         { return nil }
func (c *stubContext) ClientIP() string               { return "" }
func (c *stubContext) UserAgent() string              { return "" }
func (c *stubContext) BindJSON(any) error             { return nil }
func (c *stubContext) BindQuery(any) error            { return nil }
func (c *stubContext) ShouldBindJSON(any) error       { return nil }
func (c *stubContext) SetStatus(code int)             { c.status = code }
func (c *stubContext) SetHeader(string, string)       {}
func (c *stubContext) JSON(int, JSONBody) error       { return nil }
func (c *stubContext) String(int, string) error       { return nil }
func (c *stubContext) Data(int, string, []byte) error { return nil }
func (c *stubContext) Set(key string, value ContextValue) {
	if c.storage == nil {
		c.storage = make(map[string]ContextValue)
	}
	c.storage[key] = value
}
func (c *stubContext) Get(key string) (ContextValue, bool) {
	if c.storage == nil {
		return ContextValue{}, false
	}
	v, ok := c.storage[key]
	return v, ok
}
func (c *stubContext) Required(key string) (ContextValue, error) {
	v, ok := c.Get(key)
	if !ok {
		return ContextValue{}, errors.NewCode(errors.NotFound, "missing key").WithContext("key", key)
	}
	return v, nil
}
func (c *stubContext) Abort()                                   { c.aborted = true }
func (c *stubContext) AbortWithStatus(code int)                 { c.aborted = true; c.status = code }
func (c *stubContext) AbortWithStatusJSON(code int, _ JSONBody) { c.aborted = true; c.status = code }
func (c *stubContext) IsAborted() bool                          { return c.aborted }
func (c *stubContext) RequestContext() IRequestContext          { return nil }
func (c *stubContext) SetContext(IRequestContext)               {}

// mockPermissionChecker 模拟权限检查器
type mockPermissionChecker struct {
	permissions map[string]bool
	roles       map[string]bool
}

func newMockPermissionChecker() *mockPermissionChecker {
	return &mockPermissionChecker{
		permissions: make(map[string]bool),
		roles:       make(map[string]bool),
	}
}

func (c *mockPermissionChecker) HasPermission(ctx IContext, permission string) bool {
	return c.permissions[permission]
}

func (c *mockPermissionChecker) HasRole(ctx IContext, role string) bool {
	return c.roles[role]
}

func (c *mockPermissionChecker) HasAnyPermission(ctx IContext, permissions []string) bool {
	for _, p := range permissions {
		if c.permissions[p] {
			return true
		}
	}
	return false
}

func (c *mockPermissionChecker) HasAnyRole(ctx IContext, roles []string) bool {
	for _, r := range roles {
		if c.roles[r] {
			return true
		}
	}
	return false
}

func TestRouteBuilder_Build(t *testing.T) {
	group := newMockRouteGroup()

	err := NewRouteBuilder(group).
		Route(RouteConfig{
			Path:    "/users",
			Method:  "GET",
			Handler: func(ctx IContext) error { return nil },
		}).
		Route(RouteConfig{
			Path:    "/users",
			Method:  "POST",
			Handler: func(ctx IContext) error { return nil },
		}).
		Build()

	require.NoError(t, err)
	assert.NotNil(t, group.routes["GET"]["/users"])
	assert.NotNil(t, group.routes["POST"]["/users"])
}

func TestRouteBuilder_ShorthandMethods(t *testing.T) {
	group := newMockRouteGroup()

	err := NewRouteBuilder(group).
		GET("/users", func(ctx IContext) error { return nil }).
		POST("/users", func(ctx IContext) error { return nil }).
		PUT("/users/:id", func(ctx IContext) error { return nil }).
		DELETE("/users/:id", func(ctx IContext) error { return nil }).
		PATCH("/users/:id", func(ctx IContext) error { return nil }).
		Build()

	require.NoError(t, err)
	assert.NotNil(t, group.routes["GET"]["/users"])
	assert.NotNil(t, group.routes["POST"]["/users"])
	assert.NotNil(t, group.routes["PUT"]["/users/:id"])
	assert.NotNil(t, group.routes["DELETE"]["/users/:id"])
	assert.NotNil(t, group.routes["PATCH"]["/users/:id"])
}

func TestRouteBuilder_ValidationErrors(t *testing.T) {
	group := newMockRouteGroup()

	// 空路径
	err := NewRouteBuilder(group).
		Route(RouteConfig{
			Path:    "",
			Method:  "GET",
			Handler: func(ctx IContext) error { return nil },
		}).
		Build()

	require.Error(t, err)

	// 空方法
	err = NewRouteBuilder(group).
		Route(RouteConfig{
			Path:    "/users",
			Method:  "",
			Handler: func(ctx IContext) error { return nil },
		}).
		Build()

	require.Error(t, err)

	// 空处理器
	err = NewRouteBuilder(group).
		Route(RouteConfig{
			Path:   "/users",
			Method: "GET",
		}).
		Build()

	require.Error(t, err)
}

func TestRouteBuilder_UnknownMethod_ReturnsError(t *testing.T) {
	group := newMockRouteGroup()

	err := NewRouteBuilder(group).
		Route(RouteConfig{
			Path:    "/users",
			Method:  "TRACE",
			Handler: func(ctx IContext) error { return nil },
		}).
		Build()

	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.InvalidInput))
}

func TestRouteBuilder_GetRoutes(t *testing.T) {
	group := newMockRouteGroup()

	builder := NewRouteBuilder(group).
		Route(RouteConfig{
			Path:        "/users",
			Method:      "GET",
			Handler:     func(ctx IContext) error { return nil },
			Permissions: []string{"user:read"},
			Description: "List users",
			Tags:        []string{"user"},
		}).
		Route(RouteConfig{
			Path:        "/users",
			Method:      "POST",
			Handler:     func(ctx IContext) error { return nil },
			Permissions: []string{"user:write"},
			Roles:       []string{"admin"},
			Description: "Create user",
			Tags:        []string{"user"},
		})

	routes := builder.Routes()
	assert.Len(t, routes, 2)

	assert.Equal(t, "/users", routes[0].Path)
	assert.Equal(t, "GET", routes[0].Method)
	assert.Equal(t, []string{"user:read"}, routes[0].Permissions)

	assert.Equal(t, "/users", routes[1].Path)
	assert.Equal(t, "POST", routes[1].Method)
	assert.Equal(t, []string{"user:write"}, routes[1].Permissions)
	assert.Equal(t, []string{"admin"}, routes[1].Roles)
}

func TestRouteBuilder_WithPermissionChecker(t *testing.T) {
	group := newMockRouteGroup()
	checker := newMockPermissionChecker()
	checker.permissions["user:read"] = true

	err := NewRouteBuilder(group).
		WithPermissionChecker(checker).
		Route(RouteConfig{
			Path:        "/users",
			Method:      "GET",
			Handler:     func(ctx IContext) error { return nil },
			Permissions: []string{"user:read"},
		}).
		Build()

	require.NoError(t, err)
	assert.NotNil(t, group.routes["GET"]["/users"])
}

func TestRouteBuilder_MiddlewareChain(t *testing.T) {
	group := newMockRouteGroup()

	var order []string
	global := func(ctx IContext, next func() error) error {
		order = append(order, "global_before")
		err := next()
		order = append(order, "global_after")
		return err
	}
	route := func(ctx IContext, next func() error) error {
		order = append(order, "route_before")
		err := next()
		order = append(order, "route_after")
		return err
	}
	handler := func(ctx IContext) error {
		order = append(order, "handler")
		return nil
	}

	err := NewRouteBuilder(group).
		WithMiddlewares(global).
		Route(RouteConfig{
			Path:        "/users/123",
			Method:      "GET",
			Handler:     handler,
			Middlewares: []Middleware{route},
		}).
		Build()
	require.NoError(t, err)

	h := group.routes["GET"]["/users/123"]
	require.NotNil(t, h)
	require.NoError(t, h(&stubContext{}))

	assert.Equal(t, []string{
		"global_before",
		"route_before",
		"handler",
		"route_after",
		"global_after",
	}, order)
}

func TestRouteBuilder_PermissionDenied_StopsHandler(t *testing.T) {
	group := newMockRouteGroup()
	checker := newMockPermissionChecker()

	handlerCalled := false
	err := NewRouteBuilder(group).
		WithPermissionChecker(checker).
		Route(RouteConfig{
			Path:        "/users",
			Method:      "GET",
			Handler:     func(ctx IContext) error { handlerCalled = true; return nil },
			Permissions: []string{"user:read"},
		}).
		Build()
	require.NoError(t, err)

	h := group.routes["GET"]["/users"]
	require.NotNil(t, h)

	gotErr := h(&stubContext{})
	require.Error(t, gotErr)
	assert.True(t, errors.Is(gotErr, errors.Forbidden))
	assert.False(t, handlerCalled)
}

func TestPermissionMiddleware(t *testing.T) {
	// nil checker should not panic
	require.NotPanics(t, func() {
		err := PermissionMiddleware(nil, "user:read")(&stubContext{}, func() error { return nil })
		require.Error(t, err)
		assert.True(t, errors.Is(err, errors.InvalidInput))
	})

	checker := newMockPermissionChecker()
	checker.permissions["user:read"] = true

	mw := PermissionMiddleware(checker, "user:read")

	// 模拟有权限的情况
	nextCalled := false
	err := mw(nil, func() error {
		nextCalled = true
		return nil
	})

	require.NoError(t, err)
	assert.True(t, nextCalled)

	// 模拟无权限的情况
	mw2 := PermissionMiddleware(checker, "user:write")
	nextCalled = false
	err = mw2(nil, func() error {
		nextCalled = true
		return nil
	})

	require.Error(t, err)
	assert.False(t, nextCalled)
}

func TestRoleMiddleware(t *testing.T) {
	// nil checker should not panic
	require.NotPanics(t, func() {
		err := RoleMiddleware(nil, "admin")(&stubContext{}, func() error { return nil })
		require.Error(t, err)
		assert.True(t, errors.Is(err, errors.InvalidInput))
	})

	checker := newMockPermissionChecker()
	checker.roles["admin"] = true

	mw := RoleMiddleware(checker, "admin")

	// 模拟有角色的情况
	nextCalled := false
	err := mw(nil, func() error {
		nextCalled = true
		return nil
	})

	require.NoError(t, err)
	assert.True(t, nextCalled)

	// 模拟无角色的情况
	mw2 := RoleMiddleware(checker, "superadmin")
	nextCalled = false
	err = mw2(nil, func() error {
		nextCalled = true
		return nil
	})

	require.Error(t, err)
	assert.False(t, nextCalled)
}
