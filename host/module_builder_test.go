package host

import (
	"context"
	dibasic "gochen/di/basic"
	"reflect"
	"strings"
	"testing"

	"gochen/di"
	"gochen/domain"
	deventsourced "gochen/domain/eventsourced"
	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/projection"
	hostmodule "gochen/host/module"
	moduleasm "gochen/host/module/assembly"
	"gochen/host/module/runtimecap"
	"gochen/httpx"
	"gochen/messaging"
)

// mockRouteGroup 模拟路由组
type mockRouteGroup struct {
	routes      []string
	middlewares []httpx.Middleware
	subGroups   []*mockRouteGroup
}

func newMockRouteGroup() *mockRouteGroup {
	return &mockRouteGroup{}
}

func (g *mockRouteGroup) GET(path string, handler httpx.Handler) httpx.IRouteGroup {
	g.routes = append(g.routes, "GET:"+path)
	return g
}

func (g *mockRouteGroup) POST(path string, handler httpx.Handler) httpx.IRouteGroup {
	g.routes = append(g.routes, "POST:"+path)
	return g
}

func (g *mockRouteGroup) PUT(path string, handler httpx.Handler) httpx.IRouteGroup {
	g.routes = append(g.routes, "PUT:"+path)
	return g
}

func (g *mockRouteGroup) DELETE(path string, handler httpx.Handler) httpx.IRouteGroup {
	g.routes = append(g.routes, "DELETE:"+path)
	return g
}

func (g *mockRouteGroup) PATCH(path string, handler httpx.Handler) httpx.IRouteGroup {
	g.routes = append(g.routes, "PATCH:"+path)
	return g
}

func (g *mockRouteGroup) HEAD(path string, handler httpx.Handler) httpx.IRouteGroup {
	g.routes = append(g.routes, "HEAD:"+path)
	return g
}

func (g *mockRouteGroup) OPTIONS(path string, handler httpx.Handler) httpx.IRouteGroup {
	g.routes = append(g.routes, "OPTIONS:"+path)
	return g
}

func (g *mockRouteGroup) Group(prefix string) httpx.IRouteGroup {
	sub := newMockRouteGroup()
	g.subGroups = append(g.subGroups, sub)
	return sub
}

func (g *mockRouteGroup) Use(middleware ...httpx.Middleware) httpx.IRouteGroup {
	g.middlewares = append(g.middlewares, middleware...)
	return g
}

// mockTransport 模拟传输层
type mockTransport struct {
	subscriptions map[string][]messaging.IMessageHandler
	startCalls    int
}

func newMockTransport() *mockTransport {
	return &mockTransport{
		subscriptions: make(map[string][]messaging.IMessageHandler),
	}
}

func (t *mockTransport) Publish(ctx context.Context, message messaging.IMessage) error {
	return nil
}

func (t *mockTransport) PublishAll(ctx context.Context, messages []messaging.IMessage) error {
	return nil
}

func (t *mockTransport) Subscribe(ctx context.Context, messageType string, handler messaging.IMessageHandler) (messaging.UnsubscribeFunc, error) {
	t.subscriptions[messageType] = append(t.subscriptions[messageType], handler)
	return func(ctx context.Context) error {
		// 简单实现：不做实际取消
		return nil
	}, nil
}

func (t *mockTransport) Start(ctx context.Context) error {
	t.startCalls++
	return nil
}

func (t *mockTransport) Stop(context.Context) error {
	return nil
}

func (t *mockTransport) Stats() messaging.TransportStats {
	return messaging.TransportStats{}
}

// testService 测试用服务
type testService struct {
	name string
}

func NewTestService() *testService {
	return &testService{name: "test"}
}

func (s *testService) Name() string {
	return s.name
}

// testRouteRegistrar 测试用路由注册器
type testRouteRegistrar struct {
	service *testService
}

func NewTestRouteRegistrar(svc *testService) *testRouteRegistrar {
	return &testRouteRegistrar{service: svc}
}

func (r *testRouteRegistrar) RegisterRoutes(group httpx.IRouteGroup) error {
	group.GET("/test", nil)
	group.POST("/test", nil)
	return nil
}

// testEventHandler 测试用事件处理器
type testEventHandler struct {
	service *testService
	handled int
	msgType string
}

func NewTestEventHandler(svc *testService) *testEventHandler {
	return &testEventHandler{service: svc, msgType: "test.event"}
}

func (h *testEventHandler) Type() string {
	return h.msgType
}

func (h *testEventHandler) Handle(ctx context.Context, msg messaging.IMessage) error {
	h.handled++
	return nil
}

type multiTypeEventHandler struct {
	msgTypes []string
}

func NewMultiTypeEventHandler() *multiTypeEventHandler {
	return &multiTypeEventHandler{msgTypes: []string{"a.event", "b.event"}}
}

func (h *multiTypeEventHandler) Type() string { return "multiTypeEventHandler" }

func (h *multiTypeEventHandler) Handle(context.Context, messaging.IMessage) error { return nil }

func (h *multiTypeEventHandler) EventTypes() []string { return h.msgTypes }

type emptyTypeEventHandler struct{}

func NewEmptyTypeEventHandler() *emptyTypeEventHandler { return &emptyTypeEventHandler{} }

func (h *emptyTypeEventHandler) Type() string { return "" }

func (h *emptyTypeEventHandler) Handle(context.Context, messaging.IMessage) error { return nil }

type testProjectionBase struct {
	name string
}

func (p *testProjectionBase) Name() string { return p.name }

func (p *testProjectionBase) Handle(context.Context, eventing.IEvent) error { return nil }

func (p *testProjectionBase) SupportedEventTypes() []string { return []string{"*"} }

func (p *testProjectionBase) Rebuild(context.Context, []eventing.Event[int64]) error { return nil }

func (p *testProjectionBase) Status() projection.ProjectionStatus {
	return projection.ProjectionStatus{Name: p.name, Status: "stopped"}
}

type testProjection1 struct{ testProjectionBase }
type testProjection2 struct{ testProjectionBase }

func NewTestProjection1() *testProjection1 {
	return &testProjection1{testProjectionBase: testProjectionBase{name: "p1"}}
}
func NewTestProjection2() *testProjection2 {
	return &testProjection2{testProjectionBase: testProjectionBase{name: "p2"}}
}

type testRuntimeComponent struct {
	service     *testService
	startedWith string
	startCount  int
	stopCount   int
}

func NewTestRuntimeComponent(svc *testService) *testRuntimeComponent {
	return &testRuntimeComponent{service: svc}
}

func (c *testRuntimeComponent) Start(context.Context) error {
	c.startCount++
	if c.service != nil {
		c.startedWith = c.service.Name()
	}
	return nil
}

func (c *testRuntimeComponent) Stop(context.Context) error {
	c.stopCount++
	return nil
}

type bootAggregateEvent struct{}

func (e *bootAggregateEvent) EventType() string { return "BootAggregateEvent" }

type bootAggregate struct {
	*deventsourced.EventSourcedAggregate[int64]
}

func (a *bootAggregate) ApplyBootAggregateEvent(*bootAggregateEvent) {}

type bootTaggedAggregate struct {
	*deventsourced.EventSourcedAggregate[int64] `aggregate:"boot_tagged"`
}

func (a *bootTaggedAggregate) ApplyBootAggregateEvent(*bootAggregateEvent) {}

type bootBadAggregate struct {
	*deventsourced.EventSourcedAggregate[int64]
}

func (a *bootBadAggregate) Handle1(*bootAggregateEvent) {}
func (a *bootBadAggregate) Handle2(*bootAggregateEvent) {}

type bootInitAggregateEvent struct{}

func (e *bootInitAggregateEvent) EventType() string { return "BootInitAggregateEvent" }

type bootInitAggregate struct {
	*deventsourced.EventSourcedAggregate[int64]
}

func (a *bootInitAggregate) ApplyBootInitAggregateEvent(*bootInitAggregateEvent) {}

var _ domain.IDomainEvent = (*bootAggregateEvent)(nil)
var _ domain.IDomainEvent = (*bootInitAggregateEvent)(nil)

type mockProjectionManager struct {
	registered []string
	started    []string
	stopped    []string
}

func (m *mockProjectionManager) RegisterProjectionAny(ctx context.Context, p any) error {
	projection, ok := p.(projection.IProjection[int64])
	if !ok {
		return errors.NewCode(errors.InvalidInput, "projection type mismatch")
	}
	return m.RegisterProjectionWithContext(ctx, projection)
}

func (m *mockProjectionManager) RegisterProjectionWithContext(ctx context.Context, p projection.IProjection[int64]) error {
	m.registered = append(m.registered, p.Name())
	return nil
}

func (m *mockProjectionManager) StartProjection(name string) error {
	m.started = append(m.started, name)
	return nil
}

func (m *mockProjectionManager) StopProjection(name string) error {
	m.stopped = append(m.stopped, name)
	return nil
}

func mustBuildHostModule(t *testing.T, builder *Builder) IModule {
	t.Helper()
	module, err := builder.Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	return module
}

func newModuleDescriptorForTest(t *testing.T, builder *Builder) (moduleasm.ModuleDescriptor, error) {
	t.Helper()
	return builder.descriptor()
}

func TestModuleBuilder_IDAndName(t *testing.T) {
	module := mustBuildHostModule(t, Module("test-module").Name("Test Module"))

	if module.ID() != "test-module" {
		t.Errorf("expected ID 'test-module', got '%s'", module.ID())
	}

	if module.Name() != "Test Module" {
		t.Errorf("expected Name 'Test Module', got '%s'", module.Name())
	}
}

func TestModuleBuilder_Init(t *testing.T) {
	container := dibasic.New()

	module := mustBuildHostModule(t,
		Module("test").
			Name("Test").
			Provide(NewTestService),
	)

	opts := hostmodule.NewModuleInitOptions(container, container, container)

	err := module.Init(opts)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// 验证 provider 已注册
	var svc *testService
	err = container.Invoke(di.NewInvocation(func(s *testService) {
		svc = s
	}))
	if err != nil {
		t.Fatalf("Failed to resolve testService: %v", err)
	}
	if svc == nil {
		t.Error("testService is nil")
	}
}

func TestModuleBuilder_RegisterRoutes(t *testing.T) {
	container := dibasic.New()
	routeGroup := newMockRouteGroup()

	testMiddleware := func(ctx httpx.IContext, next func() error) error {
		return next()
	}

	module := mustBuildHostModule(t,
		Module("test").
			Name("Test").
			Provide(NewTestService).
			RouteRegistrar(NewTestRouteRegistrar).
			Middleware(testMiddleware),
	)

	opts := runtimecap.WithHTTP(
		hostmodule.NewModuleInitOptions(container, container, container),
		runtimecap.NewModuleHTTPOptions(routeGroup, ""),
	)

	err := module.Init(opts)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	routeModule, ok := module.(hostmodule.IRouteModule)
	if !ok {
		t.Fatal("module does not implement hostmodule.IRouteModule")
	}

	err = routeModule.RegisterRoutes(context.Background())
	if err != nil {
		t.Fatalf("RegisterRoutes failed: %v", err)
	}

	// 验证中间件已应用（应用到子组上）
	// MountGroup() 创建子组并应用中间件
	if len(routeGroup.subGroups) == 0 {
		t.Fatal("expected subgroup to be created")
	}
	subGroup := routeGroup.subGroups[0]
	if len(subGroup.middlewares) == 0 {
		t.Error("middleware was not applied to subgroup")
	}
}

func TestNewModule_RegisterRoutes_ResolveRoleByExplicitServiceType(t *testing.T) {
	container := dibasic.New()
	routeGroup := newMockRouteGroup()

	module := moduleasm.NewModule(moduleasm.ModuleDescriptor{
		ID:   "test",
		Name: "Test",
		Registrations: []moduleasm.Registration{
			{
				Lifetime:    moduleasm.SingletonLifetime,
				ServiceType: reflect.TypeOf((*testService)(nil)),
				Factory:     di.NewFactory(NewTestService),
			},
			{
				Lifetime:    moduleasm.SingletonLifetime,
				ServiceType: reflect.TypeOf((*moduleasm.IRouteRegistrar)(nil)).Elem(),
				Factory:     di.NewFactory(NewTestRouteRegistrar),
				Roles:       []moduleasm.Role{moduleasm.RoleRouteRegistrar},
			},
		},
	})

	opts := runtimecap.WithHTTP(
		hostmodule.NewModuleInitOptions(container, container, container),
		runtimecap.NewModuleHTTPOptions(routeGroup, ""),
	)

	if err := module.Init(opts); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	routeModule, ok := module.(hostmodule.IRouteModule)
	if !ok {
		t.Fatal("module does not implement hostmodule.IRouteModule")
	}

	if err := routeModule.RegisterRoutes(context.Background()); err != nil {
		t.Fatalf("RegisterRoutes failed: %v", err)
	}
}

func TestNewModuleDescriptor_AssignsCapabilityRoles(t *testing.T) {
	desc, err := newModuleDescriptorForTest(t,
		Module("test").
			Name("Test").
			EventHandler(NewTestEventHandler).
			Projection(NewTestProjection1).
			RuntimeComponent(NewTestRuntimeComponent),
	)
	if err != nil {
		t.Fatalf("NewModuleDescriptor failed: %v", err)
	}

	got := map[moduleasm.Role]int{}
	for _, reg := range desc.Registrations {
		for _, role := range reg.Roles {
			got[role]++
		}
	}

	if got[moduleasm.RoleEventHandler] != 1 {
		t.Fatalf("expected 1 event handler role, got %d", got[moduleasm.RoleEventHandler])
	}
	if got[moduleasm.RoleProjection] != 1 {
		t.Fatalf("expected 1 projection role, got %d", got[moduleasm.RoleProjection])
	}
	if got[moduleasm.RoleRuntimeComponent] != 1 {
		t.Fatalf("expected 1 runtime component role, got %d", got[moduleasm.RoleRuntimeComponent])
	}
}

func TestModuleBuilder_NilProviders(t *testing.T) {
	container := dibasic.New()

	module := mustBuildHostModule(t,
		Module("test").
			Name("Test").
			Provide(
				nil, // 应该被忽略
				NewTestService,
				nil, // 应该被忽略
			),
	)

	opts := hostmodule.NewModuleInitOptions(container, container, container)

	err := module.Init(opts)
	if err != nil {
		t.Fatalf("Init failed with nil providers: %v", err)
	}
}

func TestModuleBuilder_InvalidProvider_PreservesRootCause(t *testing.T) {
	_, err := Module("test").
		Name("Test").
		Provide("not-a-function").
		Build()
	if err == nil {
		t.Fatal("expected Build to fail for invalid provider")
	}

	var appErr *errors.AppError
	if !errors.As(err, &appErr) || appErr == nil {
		t.Fatalf("expected AppError, got %T: %v", err, err)
	}
	if appErr.Code() != errors.InvalidInput {
		t.Fatalf("expected InvalidInput, got %v", appErr.Code())
	}
	if !strings.Contains(err.Error(), "provider must be a function") {
		t.Fatalf("expected original provider error, got: %v", err)
	}
	if got := appErr.Details()["field"]; got != "providers" {
		t.Fatalf("expected field detail providers, got %#v", got)
	}
	if got := appErr.Details()["index"]; got != 0 {
		t.Fatalf("expected index detail 0, got %#v", got)
	}
	if got := appErr.Details()["module"]; got != "test" {
		t.Fatalf("expected module detail test, got %#v", got)
	}
}

func TestModuleBuilder_NoHTTP(t *testing.T) {
	container := dibasic.New()

	module := mustBuildHostModule(t,
		Module("test").
			Name("Test").
			RouteRegistrar(NewTestRouteRegistrar),
	)

	opts := runtimecap.WithHTTP(hostmodule.NewModuleInitOptions(container, container, container), nil) // HTTP 未启用

	// 需要先注册依赖
	container.RegisterConstructor(di.NewConstructor(NewTestService))

	err := module.Init(opts)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	routeModule := module.(hostmodule.IRouteModule)
	err = routeModule.RegisterRoutes(context.Background())
	if err != nil {
		t.Fatalf("RegisterRoutes should not fail when HTTP is nil: %v", err)
	}
}

func TestModuleBuilder_Aggregates_RegisterMetadataFailFast(t *testing.T) {
	_, err := Module("test").
		Name("Test").
		Aggregate(Aggregate(&bootBadAggregate{}, "boot_bad")).
		Build()
	if err == nil {
		t.Fatal("expected Build to fail for invalid aggregate metadata")
	}

	var appErr *errors.AppError
	if !errors.As(err, &appErr) || appErr == nil {
		t.Fatalf("expected AppError, got %T: %v", err, err)
	}
	if appErr.Code() != errors.Conflict {
		t.Fatalf("expected Conflict, got %v", appErr.Code())
	}
	if got := appErr.Details()["field"]; got != "aggregates" {
		t.Fatalf("expected field detail aggregates, got %#v", got)
	}
	if got := appErr.Details()["index"]; got != 0 {
		t.Fatalf("expected index detail 0, got %#v", got)
	}
}

func TestAggregateFromTag_CapturesResolveError(t *testing.T) {
	registration := AggregateFromTag(&bootBadAggregate{})
	if registration.Error == nil {
		t.Fatal("expected AggregateFromTag to capture tag resolve error")
	}
	if registration.AggregateType != "" {
		t.Fatalf("expected empty aggregate type, got %q", registration.AggregateType)
	}
}

func TestAggregateFromTag_ResolvesTaggedAggregate(t *testing.T) {
	registration := AggregateFromTag(&bootTaggedAggregate{})
	if registration.Error != nil {
		t.Fatalf("unexpected error: %v", registration.Error)
	}
	if registration.AggregateType != "boot_tagged" {
		t.Fatalf("expected aggregate type boot_tagged, got %q", registration.AggregateType)
	}
}

func TestModuleBuilder_Aggregates_ValidateForRuntimeInit(t *testing.T) {
	metadataRegistry := deventsourced.NewMetadataRegistry()
	module := mustBuildHostModule(t,
		Module("test").
			Name("Test").
			Aggregate(Aggregate(&bootAggregate{}, "boot_aggregate")),
	)

	container := dibasic.New()
	if err := module.Init(hostmodule.NewModuleInitOptions(container, container, container, runtimecap.MetadataRegistry(metadataRegistry))); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	meta1, err := metadataRegistry.Resolve(&bootAggregate{}, "boot_aggregate")
	if err != nil {
		t.Fatalf("MetadataRegistry.Resolve failed: %v", err)
	}
	meta2, err := metadataRegistry.Register(&bootAggregate{}, "boot_aggregate")
	if err != nil {
		t.Fatalf("MetadataRegistry.Register failed: %v", err)
	}
	if meta1 != meta2 {
		t.Fatalf("expected boot aggregate metadata to reuse the registered cache")
	}

	agg := &bootAggregate{}
	esa, err := deventsourced.InitAggregate[int64](metadataRegistry, agg, 1, "boot_aggregate")
	if err != nil {
		t.Fatalf("InitAggregate failed: %v", err)
	}
	agg.EventSourcedAggregate = esa
	if err := agg.ApplyAndRecord(&bootAggregateEvent{}); err != nil {
		t.Fatalf("ApplyAndRecord failed: %v", err)
	}
}

func TestModuleBuilder_Aggregates_DoNotRegisterBeforeInit(t *testing.T) {
	metadataRegistry := deventsourced.NewMetadataRegistry()
	module := mustBuildHostModule(t,
		Module("test").
			Name("Test").
			Aggregate(Aggregate(&bootInitAggregate{}, "boot_init_aggregate")),
	)

	if _, err := metadataRegistry.Resolve(&bootInitAggregate{}, "boot_init_aggregate"); err == nil {
		t.Fatal("expected BuildModule to avoid registering aggregate metadata before Init")
	}

	container := dibasic.New()
	if err := module.Init(hostmodule.NewModuleInitOptions(container, container, container, runtimecap.MetadataRegistry(metadataRegistry))); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	meta, err := metadataRegistry.Resolve(&bootInitAggregate{}, "boot_init_aggregate")
	if err != nil {
		t.Fatalf("MetadataRegistry.Resolve after Init failed: %v", err)
	}
	if meta == nil {
		t.Fatal("expected metadata to be registered during Init")
	}
}

func TestNewModuleDescriptor_Aggregates_RegisterAtInit(t *testing.T) {
	metadataRegistry := deventsourced.NewMetadataRegistry()
	desc, err := newModuleDescriptorForTest(t,
		Module("test").
			Name("Test").
			Aggregate(Aggregate(&bootInitAggregate{}, "boot_descriptor_aggregate")),
	)
	if err != nil {
		t.Fatalf("NewModuleDescriptor failed: %v", err)
	}

	if _, err := metadataRegistry.Resolve(&bootInitAggregate{}, "boot_descriptor_aggregate"); err == nil {
		t.Fatal("expected NewModuleDescriptor to avoid registering aggregate metadata before Init")
	}

	module := moduleasm.NewModule(desc)
	container := dibasic.New()
	if err := module.Init(hostmodule.NewModuleInitOptions(container, container, container, runtimecap.MetadataRegistry(metadataRegistry))); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	meta, err := metadataRegistry.Resolve(&bootInitAggregate{}, "boot_descriptor_aggregate")
	if err != nil {
		t.Fatalf("MetadataRegistry.Resolve after descriptor Init failed: %v", err)
	}
	if meta == nil {
		t.Fatal("expected descriptor init to register aggregate metadata")
	}
}
