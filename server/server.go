package server

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strings"

	"gochen/app"
	"gochen/di"
	"gochen/eventing/bus"
	"gochen/eventing/projection"
	httpx "gochen/http"
	hbasic "gochen/http/basic"
	"gochen/messaging"
	"gochen/messaging/transport/memory"
)

// IRouteRegistrar 约定的路由注册器接口。
//
// gochen-iam / gochen-llm 等模块中的路由构造器只要实现了这三个方法，
// 就可以被 Server 自动发现并挂载到 HTTP 路由树上。
type IRouteRegistrar interface {
	RegisterRoutes(group httpx.IRouteGroup)
	GetName() string
	GetPriority() int
}

// ServerConfig 定义模块级 Server 的配置。
//
// Server 关注“如何把一组 domain.IModule 跑起来”，包括：
//   - HTTP 监听地址（Host/Port/BasePath）
//   - DI 容器（默认使用 di.NewBasic）
//   - 事件总线（默认使用内存 MessageBus + MemoryTransport）
//   - HTTP 服务器实现（默认使用 http/basic.HttpServer）
type ServerConfig struct {
	// 服务标识，用于日志与健康检查
	Name string

	// HTTP 监听配置
	Host     string
	Port     int
	BasePath string

	// 可选：外部注入的基础组件
	Container  di.IContainer
	EventBus   bus.IEventBus
	HTTPServer httpx.IHttpServer
}

// DefaultServerConfig 返回带合理默认值的配置：
//   - Name: "gochen-module-server"
//   - Host: "0.0.0.0"
//   - Port: 8080
//   - BasePath: "/api/v1"
//   - Container: di.NewBasic()
func DefaultServerConfig() *ServerConfig {
	return &ServerConfig{
		Name:      "gochen-module-server",
		Host:      "0.0.0.0",
		Port:      8080,
		BasePath:  "/api/v1",
		Container: di.NewBasic(),
	}
}

// ServerOption 用于按需修改 ServerConfig。
type ServerOption func(*ServerConfig)

// WithServerName 覆盖服务名称。
func WithServerName(name string) ServerOption {
	return func(cfg *ServerConfig) {
		if name != "" {
			cfg.Name = name
		}
	}
}

// WithServerHost 设置 HTTP 监听 Host。
func WithServerHost(host string) ServerOption {
	return func(cfg *ServerConfig) {
		if host != "" {
			cfg.Host = host
		}
	}
}

// WithServerPort 设置 HTTP 监听端口。
func WithServerPort(port int) ServerOption {
	return func(cfg *ServerConfig) {
		if port > 0 {
			cfg.Port = port
		}
	}
}

// WithServerBasePath 设置模块路由的基础前缀（例如 "/api/v1"）。
func WithServerBasePath(basePath string) ServerOption {
	return func(cfg *ServerConfig) {
		if basePath == "" {
			return
		}
		if !strings.HasPrefix(basePath, "/") {
			basePath = "/" + basePath
		}
		cfg.BasePath = basePath
	}
}

// WithServerContainer 注入自定义 DI 容器。
//
// 典型用法：在外部提前注册数据库 / ORM / 配置等基础设施，再交给 Server 继续装配领域模块。
func WithServerContainer(container di.IContainer) ServerOption {
	return func(cfg *ServerConfig) {
		if container != nil {
			cfg.Container = container
		}
	}
}

// WithServerEventBus 注入自定义事件总线实现。
func WithServerEventBus(eventBus bus.IEventBus) ServerOption {
	return func(cfg *ServerConfig) {
		if eventBus != nil {
			cfg.EventBus = eventBus
		}
	}
}

// WithServerHTTPServer 注入自定义 IHttpServer 实现（例如 Gin 适配器）。
func WithServerHTTPServer(httpServer httpx.IHttpServer) ServerOption {
	return func(cfg *ServerConfig) {
		if httpServer != nil {
			cfg.HTTPServer = httpServer
		}
	}
}

// Server 是一个轻量的 IServer 实现，用于承载一组 domain.IModule。
//
// 典型场景：
//   - 为 gochen-iam / gochen-llm 提供最小可运行环境；
//   - 独立启动单一领域模块做集成测试或管理后台。
//
// 它不负责复杂配置解析，只假设：
//   - DI 容器中可以注册/解析模块所需的依赖；
//   - 模块通过 RegisterProviders 注册仓储/服务/路由构造器；
//   - 路由构造器实现 RouteRegistrar 接口。
type Server struct {
	config    *ServerConfig
	modules   []app.IModule
	container di.IContainer

	httpServer httpx.IHttpServer
	eventBus   bus.IEventBus

	transport         messaging.ITransport
	projectionManager *projection.ProjectionManager
	projectionNames   []string
}

// NewServer 创建模块级 Server。
//
// modules 通常是若干实现了 domain.IModule 的领域模块，例如：
//   - gochen-iam.NewModule()
//   - gochen-llm.NewModule()
func NewServer(modules []app.IModule, opts ...ServerOption) *Server {
	cfg := DefaultServerConfig()
	for _, opt := range opts {
		opt(cfg)
	}
	if cfg.Container == nil {
		cfg.Container = di.NewBasic()
	}
	return &Server{
		config:    cfg,
		modules:   modules,
		container: cfg.Container,
	}
}

// Name 实现 IServer.Name
func (s *Server) Name() string {
	if s.config != nil && s.config.Name != "" {
		return s.config.Name
	}
	return "gochen-module-server"
}

// LoadConfig 实现 IServer.LoadConfig。
//
// 轻量方案下不做文件/环境变量解析，仅补齐默认值与规范化 BasePath。
func (s *Server) LoadConfig() error {
	if s.config == nil {
		s.config = DefaultServerConfig()
	}
	if s.config.BasePath == "" {
		s.config.BasePath = "/api/v1"
	}
	if !strings.HasPrefix(s.config.BasePath, "/") {
		s.config.BasePath = "/" + s.config.BasePath
	}
	if s.config.Container == nil {
		s.config.Container = di.NewBasic()
	}
	s.container = s.config.Container
	return nil
}

// SetupDependencies 实现 IServer.SetupDependencies。
//
// 主要步骤：
//  1. 确保 EventBus 与消息传输层存在（若调用方未注入则创建内存实现）；
//  2. 依次调用各 domain.IModule.RegisterProviders 注册仓储/服务/路由构造器；
//  3. 调用各模块 RegisterEventHandlers / RegisterProjections（若有）；
//  4. 创建 IHttpServer 并聚合所有 RouteRegistrar 进行路由注册。
func (s *Server) SetupDependencies(ctx context.Context) error {
	if s.container == nil {
		s.container = di.NewBasic()
	}

	// 1) 事件总线（优先使用外部注入/容器中的实现，其次回退到内存实现）
	if err := s.ensureEventBus(); err != nil {
		return fmt.Errorf("failed to initialize event bus: %w", err)
	}

	// 2) 注册领域模块提供者
	for _, m := range s.modules {
		if m == nil {
			continue
		}
		if err := m.RegisterProviders(s.container); err != nil {
			return fmt.Errorf("failed to register domain module %s: %w", m.Name(), err)
		}
	}

	// 3) 注册事件处理器与投影（若模块实现）
	for _, m := range s.modules {
		if m == nil {
			continue
		}
		if s.eventBus != nil {
			if err := m.RegisterEventHandlers(ctx, s.eventBus, s.container); err != nil {
				return fmt.Errorf("failed to register event handlers for module %s: %w", m.Name(), err)
			}
		}

		manager, names, err := m.RegisterProjections(s.container)
		if err != nil {
			return fmt.Errorf("failed to register projections for module %s: %w", m.Name(), err)
		}
		if manager != nil && s.projectionManager == nil {
			s.projectionManager = manager
		}
		if len(names) > 0 {
			s.projectionNames = append(s.projectionNames, names...)
		}
	}

	// 4) HTTP Server 与路由注册
	if err := s.ensureHTTPServer(); err != nil {
		return err
	}
	s.registerRoutes()

	return nil
}

// StartBackgroundTasks 实现 IServer.StartBackgroundTasks。
//
// 对于内存消息传输层，我们在此启动 Worker 池；其他后台任务（如投影）留给调用方按需扩展。
func (s *Server) StartBackgroundTasks(ctx context.Context) error {
	if s.transport != nil {
		if err := s.transport.Start(ctx); err != nil {
			return fmt.Errorf("failed to start message transport: %w", err)
		}
	}
	// 当前轻量方案不主动启动投影/定时任务，留给上层按需集成。
	return nil
}

// Run 实现 IServer.Run：启动 HTTP 服务并阻塞等待退出。
func (s *Server) Run(ctx context.Context) error {
	if s.httpServer == nil {
		return fmt.Errorf("HTTP server not initialized")
	}

	// 使用 WebConfig 中的 Host/Port，Run 阶段不再拼接地址
	if err := s.httpServer.Start(""); err != nil {
		// 对于基于 net/http 的实现，ErrServerClosed 视为正常退出
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
	return nil
}

// Shutdown 实现 IServer.Shutdown：优雅关闭 HTTP 服务与消息传输层。
func (s *Server) Shutdown(ctx context.Context) error {
	var firstErr error

	if s.httpServer != nil {
		if err := s.httpServer.Stop(ctx); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("failed to stop HTTP server: %w", err)
		}
	}

	if s.transport != nil {
		if err := s.transport.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("failed to close message transport: %w", err)
		}
	}

	return firstErr
}

// ensureEventBus 确保 eventBus 与 transport 已准备好。
// 优先级：
//  1. ServerConfig.EventBus（显式注入）；
//  2. 容器中已有的 IEventBus；
//  3. 自动创建 MemoryTransport + MessageBus + EventBus。
func (s *Server) ensureEventBus() error {
	if s.eventBus != nil {
		return nil
	}

	// 1) 显式注入
	if s.config != nil && s.config.EventBus != nil {
		s.eventBus = s.config.EventBus
		_ = registerInstanceByType(s.container, (*bus.IEventBus)(nil), s.eventBus)
		return nil
	}

	// 2) 容器中已有实现
	if eb, ok := tryResolveEventBus(s.container); ok {
		s.eventBus = eb
		return nil
	}

	// 3) 创建内存实现
	mt := memory.NewMemoryTransport(0, 0)
	msgBus := messaging.NewMessageBus(mt)
	s.transport = mt
	s.eventBus = bus.NewEventBus(msgBus)

	_ = registerInstanceByType(s.container, (*messaging.ITransport)(nil), mt)
	_ = registerInstanceByType(s.container, (*messaging.IMessageBus)(nil), msgBus)
	_ = registerInstanceByType(s.container, (*bus.IEventBus)(nil), s.eventBus)

	return nil
}

// ensureHTTPServer 确保 httpServer 已准备好。
// 优先级：
//  1. ServerConfig.HTTPServer（显式注入）；
//  2. 自动创建基于 net/http 的 http/basic.HttpServer。
func (s *Server) ensureHTTPServer() error {
	if s.httpServer != nil {
		return nil
	}

	if s.config != nil && s.config.HTTPServer != nil {
		s.httpServer = s.config.HTTPServer
		return nil
	}

	// 创建默认的基础 HTTP 服务器
	cfg := &httpx.WebConfig{
		Host:         s.config.Host,
		Port:         s.config.Port,
		Mode:         "debug",
		ReadTimeout:  0,
		WriteTimeout: 0,
		IdleTimeout:  0,
	}
	s.httpServer = hbasic.NewHTTPServer(cfg)
	return nil
}

// registerRoutes 从容器中收集所有 RouteRegistrar，并挂载到 HTTP Server。
func (s *Server) registerRoutes() {
	if s.httpServer == nil || s.container == nil {
		return
	}

	registrars := s.collectRouteRegistrars()
	if len(registrars) == 0 {
		return
	}

	basePath := s.config.BasePath
	if basePath == "/" {
		basePath = ""
	}
	group := s.httpServer.Group(basePath)

	for _, r := range registrars {
		r.RegisterRoutes(group)
	}

	// 附带一个简单的健康检查路由
	group.GET("/health", func(ctx httpx.IHttpContext) error {
		return ctx.JSON(http.StatusOK, map[string]any{
			"status":  "ok",
			"service": s.Name(),
		})
	})
}

// collectRouteRegistrars 遍历容器中所有服务，筛选出实现 RouteRegistrar 的实例并按优先级排序。
func (s *Server) collectRouteRegistrars() []IRouteRegistrar {
	if s.container == nil {
		return nil
	}

	names := s.container.GetRegisteredNames()
	registrars := make([]IRouteRegistrar, 0)

	for _, name := range names {
		inst, err := s.container.Resolve(name)
		if err != nil {
			continue
		}
		if r, ok := inst.(IRouteRegistrar); ok {
			registrars = append(registrars, r)
		}
	}

	sort.Slice(registrars, func(i, j int) bool {
		pi, pj := registrars[i].GetPriority(), registrars[j].GetPriority()
		if pi == pj {
			return registrars[i].GetName() < registrars[j].GetName()
		}
		return pi < pj
	})

	return registrars
}

// tryResolveEventBus 尝试从容器解析 IEventBus。
func tryResolveEventBus(container di.IContainer) (bus.IEventBus, bool) {
	if container == nil {
		return nil, false
	}
	t := reflect.TypeOf((*bus.IEventBus)(nil)).Elem()
	candidates := []string{t.String(), t.Name()}

	for _, name := range candidates {
		if !container.IsRegistered(name) {
			continue
		}
		inst, err := container.Resolve(name)
		if err != nil {
			continue
		}
		if eb, ok := inst.(bus.IEventBus); ok && eb != nil {
			return eb, true
		}
	}
	return nil, false
}

// registerInstanceByType 按接口类型字符串注册实例到容器（若尚未注册）。
func registerInstanceByType(container di.IContainer, ifacePtr any, instance any) error {
	if container == nil || ifacePtr == nil || instance == nil {
		return nil
	}

	t := reflect.TypeOf(ifacePtr)
	if t.Kind() != reflect.Ptr {
		return nil
	}
	iface := t.Elem()
	name := iface.String()
	if container.IsRegistered(name) {
		return nil
	}
	return container.RegisterInstance(name, instance)
}
