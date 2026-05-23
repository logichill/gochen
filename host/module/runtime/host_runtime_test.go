package runtime

import (
	"context"
	"errors"
	"testing"

	"gochen/host/internal/bootstrap"
	"gochen/host/module/runtimecap"
	"gochen/httpx"
	"gochen/messaging"
)

type testModule struct {
	id       string
	name     string
	initFn   func(ModuleInitOptions) error
	startFn  func(context.Context) (ModuleStopFunc, error)
	routesFn func(context.Context) error
}

func (m *testModule) ID() string   { return m.id }
func (m *testModule) Name() string { return m.name }
func (m *testModule) Init(opts ModuleInitOptions) error {
	if m.initFn != nil {
		return m.initFn(opts)
	}
	return nil
}
func (m *testModule) Start(ctx context.Context) (ModuleStopFunc, error) {
	if m.startFn != nil {
		return m.startFn(ctx)
	}
	return nil, nil
}
func (m *testModule) RegisterRoutes(ctx context.Context) error {
	if m.routesFn != nil {
		return m.routesFn(ctx)
	}
	return nil
}

type testTransport struct {
	startErr              error
	stopErr               error
	startCalls            int
	stopCalls             int
	stopWithSnapshotCalls int
	running               bool
}

func (t *testTransport) Subscribe(context.Context, string, messaging.IMessageHandler) (messaging.UnsubscribeFunc, error) {
	return func(context.Context) error { return nil }, nil
}
func (t *testTransport) Start(context.Context) error {
	t.startCalls++
	if t.startErr == nil {
		t.running = true
	}
	return t.startErr
}
func (t *testTransport) Stop(context.Context) error {
	t.stopCalls++
	if t.stopErr == nil {
		t.running = false
	}
	return t.stopErr
}
func (t *testTransport) StopWithSnapshot(context.Context) ([]messaging.IMessage, error) {
	t.stopWithSnapshotCalls++
	if t.stopErr == nil {
		t.running = false
	}
	return nil, t.stopErr
}
func (t *testTransport) Stats() messaging.TransportStats {
	return messaging.TransportStats{Running: t.running}
}

type conflictServer struct {
	httpx.IServer
	conflicts []httpx.RouteConflict
}

func (s *conflictServer) RouteConflicts() []httpx.RouteConflict {
	return append([]httpx.RouteConflict(nil), s.conflicts...)
}

type lifecycleServer struct {
	httpx.IServer
	stopErr   error
	stopCalls int
}

func (s *lifecycleServer) Stop(context.Context) error {
	s.stopCalls++
	return s.stopErr
}

type stopOnlyTransport struct {
	stopErr   error
	stopCalls int
}

func (t *stopOnlyTransport) Subscribe(context.Context, string, messaging.IMessageHandler) (messaging.UnsubscribeFunc, error) {
	return func(context.Context) error { return nil }, nil
}
func (t *stopOnlyTransport) Start(context.Context) error { return nil }
func (t *stopOnlyTransport) Stop(context.Context) error {
	t.stopCalls++
	return t.stopErr
}

type testHTTPServer struct{}

func (s *testHTTPServer) GET(string, httpx.Handler) httpx.IServer     { return s }
func (s *testHTTPServer) POST(string, httpx.Handler) httpx.IServer    { return s }
func (s *testHTTPServer) PUT(string, httpx.Handler) httpx.IServer     { return s }
func (s *testHTTPServer) DELETE(string, httpx.Handler) httpx.IServer  { return s }
func (s *testHTTPServer) PATCH(string, httpx.Handler) httpx.IServer   { return s }
func (s *testHTTPServer) HEAD(string, httpx.Handler) httpx.IServer    { return s }
func (s *testHTTPServer) OPTIONS(string, httpx.Handler) httpx.IServer { return s }
func (s *testHTTPServer) Group(string) httpx.IRouteGroup              { return &testRouteGroup{} }
func (s *testHTTPServer) Use(...httpx.Middleware) httpx.IServer       { return s }
func (s *testHTTPServer) Static(string, string) httpx.IServer         { return s }
func (s *testHTTPServer) ServeStatic(string, string)                  {}
func (s *testHTTPServer) Start(string) error                          { return nil }
func (s *testHTTPServer) Stop(context.Context) error                  { return nil }
func (s *testHTTPServer) HealthCheck() error                          { return nil }

type testRouteGroup struct{}

func (g *testRouteGroup) GET(string, httpx.Handler) httpx.IRouteGroup     { return g }
func (g *testRouteGroup) POST(string, httpx.Handler) httpx.IRouteGroup    { return g }
func (g *testRouteGroup) PUT(string, httpx.Handler) httpx.IRouteGroup     { return g }
func (g *testRouteGroup) DELETE(string, httpx.Handler) httpx.IRouteGroup  { return g }
func (g *testRouteGroup) PATCH(string, httpx.Handler) httpx.IRouteGroup   { return g }
func (g *testRouteGroup) HEAD(string, httpx.Handler) httpx.IRouteGroup    { return g }
func (g *testRouteGroup) OPTIONS(string, httpx.Handler) httpx.IRouteGroup { return g }
func (g *testRouteGroup) Group(string) httpx.IRouteGroup                  { return g }
func (g *testRouteGroup) Use(...httpx.Middleware) httpx.IRouteGroup       { return g }

func TestPrepareNormalizesModuleHTTPConfigKeys(t *testing.T) {
	t.Parallel()

	var gotPrefix string
	host := NewHost([]ModuleCtor{
		func() (IModule, error) {
			return &testModule{
				id:   "Users",
				name: "Users",
				initFn: func(opts ModuleInitOptions) error {
					httpOpt := runtimecap.HTTPFrom(opts)
					if httpOpt == nil {
						t.Fatal("expected module HTTP options")
					}
					gotPrefix = httpOpt.Prefix()
					return nil
				},
			}, nil
		},
	}, WithHTTPServer(&testHTTPServer{}), WithBasePath("/api"), func(cfg *HostConfig) {
		cfg.ModuleHTTP["users"] = ModuleHTTPConfig{Prefix: "/custom"}
	})

	if err := host.Prepare(context.Background()); err != nil {
		t.Fatalf("Prepare returned error: %v", err)
	}
	if gotPrefix != "/custom" {
		t.Fatalf("unexpected module HTTP prefix: %q", gotPrefix)
	}
}

func TestPrepareRejectsDuplicateNormalizedModuleHTTPKeys(t *testing.T) {
	t.Parallel()

	host := NewHost(nil, func(cfg *HostConfig) {
		cfg.ModuleHTTP["Users"] = ModuleHTTPConfig{}
		cfg.ModuleHTTP["users"] = ModuleHTTPConfig{}
	})

	err := host.normalizeModuleHTTPConfigKeys()
	if err == nil {
		t.Fatal("expected duplicate normalized key error")
	}
}

func TestStartBackgroundFailsFastOnRouteConflictsBeforeModuleStart(t *testing.T) {
	t.Parallel()

	started := false
	host := &Host{
		config: &HostConfig{FailFastOnRouteConflicts: true},
		modules: []IModule{
			&testModule{
				id:       "users",
				name:     "users",
				routesFn: func(context.Context) error { return nil },
				startFn: func(context.Context) (ModuleStopFunc, error) {
					started = true
					return nil, nil
				},
			},
		},
		runtime: &bootstrap.Runtime{
			HTTPServer: &conflictServer{
				IServer:   httpx.WithRouteRegistry(&testHTTPServer{}),
				conflicts: []httpx.RouteConflict{{Method: "GET", Path: "/users", Count: 2}},
			},
		},
	}

	err := host.StartBackground(context.Background())
	if err == nil {
		t.Fatal("expected route conflict error")
	}
	if started {
		t.Fatal("module Start should not run after route conflict fail-fast")
	}
}

func TestStartBackgroundRollsBackStartedModulesOnTransportFailure(t *testing.T) {
	t.Parallel()

	rolledBack := false
	host := &Host{
		config: &HostConfig{},
		modules: []IModule{
			&testModule{
				id:   "users",
				name: "users",
				startFn: func(context.Context) (ModuleStopFunc, error) {
					return func(context.Context) error {
						rolledBack = true
						return nil
					}, nil
				},
			},
		},
		runtime: &bootstrap.Runtime{
			Transport: &testTransport{startErr: errors.New("boom")},
		},
	}

	err := host.StartBackground(context.Background())
	if err == nil {
		t.Fatal("expected transport start error")
	}
	if !rolledBack {
		t.Fatal("expected module rollback stop to be called")
	}
	if len(host.moduleStops) != 0 {
		t.Fatalf("expected no retained module stops after rollback, got %d", len(host.moduleStops))
	}
}

func TestShutdownRetainsFailedModuleStopsAndSkipsTransportClose(t *testing.T) {
	t.Parallel()

	stopErr := errors.New("stop failed")
	server := &lifecycleServer{IServer: &testHTTPServer{}}
	transport := &testTransport{}
	host := &Host{
		runtime: &bootstrap.Runtime{
			HTTPServer: server,
			Transport:  transport,
		},
		moduleStops: []ModuleStopFunc{
			func(context.Context) error { return stopErr },
		},
	}

	err := host.Shutdown(context.Background())
	if err == nil {
		t.Fatal("expected shutdown error")
	}
	if server.stopCalls != 1 {
		t.Fatalf("expected HTTP server stop once, got %d", server.stopCalls)
	}
	if len(host.moduleStops) != 1 {
		t.Fatalf("expected failed module stop to be retained, got %d", len(host.moduleStops))
	}
	if transport.stopCalls != 0 || transport.stopWithSnapshotCalls != 0 {
		t.Fatal("transport should not close while failed module stops remain")
	}
}

func TestShutdownIgnoresAlreadyStoppedTransportConflict(t *testing.T) {
	t.Parallel()

	transport := &testTransport{
		stopErr: messaging.NewTransportAlreadyStoppedError("transport is not running"),
	}
	host := &Host{
		runtime: &bootstrap.Runtime{
			HTTPServer: &lifecycleServer{IServer: &testHTTPServer{}},
			Transport:  transport,
		},
	}

	if err := host.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown returned error: %v", err)
	}
	if transport.stopWithSnapshotCalls != 1 {
		t.Fatalf("expected StopWithSnapshot once, got %d", transport.stopWithSnapshotCalls)
	}
}

func TestShutdownIgnoresAlreadyStoppedStopOnlyTransport(t *testing.T) {
	t.Parallel()

	transport := &stopOnlyTransport{
		stopErr: messaging.NewTransportAlreadyStoppedError("transport is not running"),
	}
	host := &Host{
		runtime: &bootstrap.Runtime{
			HTTPServer: &lifecycleServer{IServer: &testHTTPServer{}},
			Transport:  transport,
		},
	}

	if err := host.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown returned error: %v", err)
	}
	if transport.stopCalls != 1 {
		t.Fatalf("expected Stop once, got %d", transport.stopCalls)
	}
}

func TestShutdownReturnsTransportStopError(t *testing.T) {
	t.Parallel()

	stopErr := errors.New("stop failed")
	transport := &stopOnlyTransport{stopErr: stopErr}
	host := &Host{
		runtime: &bootstrap.Runtime{
			HTTPServer: &lifecycleServer{IServer: &testHTTPServer{}},
			Transport:  transport,
		},
	}

	err := host.Shutdown(context.Background())
	if err == nil {
		t.Fatal("expected Shutdown to return transport stop error")
	}
	if !errors.Is(err, stopErr) {
		t.Fatalf("expected transport stop error to be preserved, got %v", err)
	}
	if transport.stopCalls != 1 {
		t.Fatalf("expected Stop once, got %d", transport.stopCalls)
	}
}
