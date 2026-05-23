package runtime

import (
	dibasic "gochen/di/basic"
	"strings"

	auth "gochen/auth"
	"gochen/di"
	deventsourced "gochen/domain/eventsourced"
	"gochen/host/capability"
	"gochen/httpx"
)

// ModuleHTTPConfig 定义模块级 HTTP 挂载配置。
//
// 说明：
// - Disabled 为 true 时，不向模块注入 HTTP（模块仍可运行其他能力）；
// - Prefix 为模块挂载前缀（相对于 Host.BasePath）；空值表示直接挂载在 BasePath group 上；
// - Middlewares 挂载在模块 group 上（发生在 Prefix group 创建之后）。
type ModuleHTTPConfig struct {
	Disabled    bool
	Prefix      string
	Middlewares []httpx.Middleware
}

// HostConfig 聚合模块化 Host 运行所需的装配配置。
type HostConfig struct {
	// 服务标识，用于日志与健康检查
	Name string

	// SecurityLayer 定义当前 Host 默认承载的“安全分层”（API/Web）。
	//
	// 说明：
	// - 该字段用于让“默认安全策略”在代码层面可见，并通过默认中间件降低误用风险；
	// - 典型：模块级 API Host 默认设置为 httpx.SecurityLayerAPI；
	// - 模块/路由如需覆盖，可在更内层的 group 上显式挂载 httpx/middleware.SecurityLayer。
	SecurityLayer httpx.SecurityLayer
	// AllowSession 控制是否允许 Session 语义（gochen/auth.SessionID）在该链路可用。
	//
	// 说明：
	// - nil 表示按 SecurityLayer 取默认值：API -> false（默认拒绝），Web -> true；
	// - 该字段仅用于“防误用”层面的语义约束，不提供 Session 的创建/验证/持久化能力。
	AllowSession *bool

	// HTTP 监听配置
	Host     string
	Port     int
	BasePath string

	// 可选：外部注入的基础组件
	Container         di.IContainer
	EventBus          capability.IEventSubscriber
	Transport         capability.ITransport
	ProjectionManager capability.IProjectionManager
	HTTPServer        httpx.IServer
	AuthzRegistry     *auth.Registry
	ModuleRegistry    *ModuleRegistry
	MetadataRegistry  *deventsourced.MetadataRegistry

	// RouteMiddlewares 配置挂载在 BasePath group 上的统一中间件（可选）。
	//
	// 典型用法：让模块完成 providers 注册后，由组合根统一配置鉴权、限流、日志等中间件，
	// 再由模块在 Start(ctx) 阶段挂载路由。
	RouteMiddlewares []httpx.Middleware

	// DisableHealthRoute 为 true 时不注册默认的健康检查路由。
	//
	// 说明：
	// - module.Host 默认会在根路由注册以下监控端点（与 eventing/monitoring 口径对齐）：
	// - - GET /healthz   : 健康报告（unhealthy -> 503）
	// - - GET /readyz    : 就绪探针（当前与 healthz 语义一致）
	// - - GET /metrics   : 指标快照（Summary）
	// - - GET /snapshot  : 聚合快照（指标 + 健康 + 可选扩展）
	// - 某些应用可能已自行挂载监控路由或需要对监控端点做鉴权/网关隔离，此时可关闭默认路由并在组合根显式装配。
	DisableHealthRoute bool

	// FailFastOnRouteConflicts 为 true 时，在启动期检测重复路由（method+path）并直接失败。
	//
	// 说明：
	// - 该校验发生在 StartBackground 的 RegisterRoutes 阶段之后、Start 阶段之前；
	// - 对于不支持路由注册记录的 HTTPServer，实现可能无法检测冲突（会跳过）。
	FailFastOnRouteConflicts bool

	// ModuleHTTP 定义模块级 HTTP 挂载配置（按模块 ID() 索引）。
	//
	// 说明：
	// - 该配置仅影响“Host 注入给 module.Init 的 HTTP options”，不包含任何模块装配逻辑；
	// - 默认行为（未配置时）由 Host 兜底：Prefix 为 "/{module.ID()}"（例如 "/iam"），用于隔离模块路由命名空间；
	// - 如需不增加模块前缀，可显式配置 `ModuleHTTPConfig{Prefix: ""}`（此时模块路由直接挂载在 BasePath group 下）。
	ModuleHTTP map[string]ModuleHTTPConfig
}

func DefaultHostConfig() *HostConfig {
	deny := false
	return &HostConfig{
		Name:             "gochen-module-server",
		SecurityLayer:    httpx.SecurityLayerAPI,
		AllowSession:     &deny,
		Host:             "0.0.0.0",
		Port:             8080,
		BasePath:         "/api/v1",
		Container:        dibasic.New(),
		ModuleHTTP:       map[string]ModuleHTTPConfig{},
		MetadataRegistry: deventsourced.NewMetadataRegistry(),
	}
}

// Option 定义服务可选配置函数。
type Option func(*HostConfig)

// WithName 覆盖服务名称。
func WithName(name string) Option {
	return func(cfg *HostConfig) {
		if name != "" {
			cfg.Name = name
		}
	}
}

// WithAddress 覆盖 HTTP 监听地址。
func WithAddress(host string) Option {
	return func(cfg *HostConfig) {
		if host != "" {
			cfg.Host = host
		}
	}
}

// WithPort 覆盖 HTTP 监听端口。
func WithPort(port int) Option {
	return func(cfg *HostConfig) {
		if port > 0 {
			cfg.Port = port
		}
	}
}

// WithBasePath 设置模块路由的基础前缀（例如 `/api/v1`）。
func WithBasePath(basePath string) Option {
	return func(cfg *HostConfig) {
		if basePath == "" {
			return
		}
		if !strings.HasPrefix(basePath, "/") {
			basePath = "/" + basePath
		}
		cfg.BasePath = basePath
	}
}

// WithContainer 注入自定义 DI 容器。
//
// 典型用法是在组合根提前注册数据库、ORM、配置等基础设施，再交给 Host 继续装配模块。
func WithContainer(container di.IContainer) Option {
	return func(cfg *HostConfig) {
		if container != nil {
			cfg.Container = container
		}
	}
}

// WithEventBus 注入自定义事件总线实现。
func WithEventBus(eventBus capability.IEventSubscriber) Option {
	return func(cfg *HostConfig) {
		if eventBus != nil {
			cfg.EventBus = eventBus
		}
	}
}

// WithTransport 注入消息传输层实现。
//
// 若 Transport 不为 nil，Host 会在 StartBackground 中调用 `Start(ctx)` 启动它。
func WithTransport(transport capability.ITransport) Option {
	return func(cfg *HostConfig) {
		if transport != nil {
			cfg.Transport = transport
		}
	}
}

// WithProjectionManager 注入自定义投影注册器。
func WithProjectionManager(manager capability.IProjectionManager) Option {
	return func(cfg *HostConfig) {
		if manager != nil {
			cfg.ProjectionManager = manager
		}
	}
}

// WithAuthzRegistry 注入模块级 authz 注册表。
func WithAuthzRegistry(registry *auth.Registry) Option {
	return func(cfg *HostConfig) {
		if registry != nil {
			cfg.AuthzRegistry = registry
		}
	}
}

// WithModuleRegistry 注入模块目录注册表。
func WithModuleRegistry(registry *ModuleRegistry) Option {
	return func(cfg *HostConfig) {
		if registry != nil {
			cfg.ModuleRegistry = registry
		}
	}
}

// WithMetadataRegistry 注入事件溯源聚合 metadata 注册表。
func WithMetadataRegistry(registry *deventsourced.MetadataRegistry) Option {
	return func(cfg *HostConfig) {
		if registry != nil {
			cfg.MetadataRegistry = registry
		}
	}
}

// WithHTTPServer 注入自定义 HTTPServer 实现。
func WithHTTPServer(httpServer httpx.IServer) Option {
	return func(cfg *HostConfig) {
		if httpServer != nil {
			cfg.HTTPServer = httpServer
		}
	}
}

// WithRouteMiddlewares 追加挂载在 BasePath group 上的统一中间件。
func WithRouteMiddlewares(middlewares ...httpx.Middleware) Option {
	return func(cfg *HostConfig) {
		if len(middlewares) == 0 {
			return
		}
		cfg.RouteMiddlewares = append(cfg.RouteMiddlewares, middlewares...)
	}
}

// WithDisableHealthRoute 控制是否跳过框架默认注册的健康检查路由。
func WithDisableHealthRoute(disable bool) Option {
	return func(cfg *HostConfig) {
		if cfg == nil {
			return
		}
		cfg.DisableHealthRoute = disable
	}
}

// WithFailFastOnRouteConflicts 在启动期检测重复路由（method+path）并直接失败。
func WithFailFastOnRouteConflicts(enabled bool) Option {
	return func(cfg *HostConfig) {
		if cfg == nil {
			return
		}
		cfg.FailFastOnRouteConflicts = enabled
	}
}

// WithModuleHTTP 设置指定模块的 HTTP 挂载配置（按模块 ID() 索引）。
func WithModuleHTTP(moduleID string, cfgOverride ModuleHTTPConfig) Option {
	return func(cfg *HostConfig) {
		moduleID = strings.TrimSpace(moduleID)
		if cfg == nil || moduleID == "" {
			return
		}
		if cfg.ModuleHTTP == nil {
			cfg.ModuleHTTP = map[string]ModuleHTTPConfig{}
		}
		cfg.ModuleHTTP[moduleID] = cfgOverride
	}
}

// WithSecurityLayer 显式指定服务默认采用的安全分层。
func WithSecurityLayer(layer httpx.SecurityLayer) Option {
	return func(cfg *HostConfig) {
		if cfg == nil {
			return
		}
		if layer == "" {
			return
		}
		cfg.SecurityLayer = layer
	}
}

// WithAllowSession 控制 API 安全层是否允许携带会话语义。
func WithAllowSession(allow bool) Option {
	return func(cfg *HostConfig) {
		if cfg == nil {
			return
		}
		cfg.AllowSession = &allow
	}
}

func ensureHostConfig(cfg *HostConfig) *HostConfig {
	if cfg == nil {
		cfg = DefaultHostConfig()
	}
	if cfg.BasePath == "" {
		cfg.BasePath = "/api/v1"
	}
	if cfg.SecurityLayer == "" {
		cfg.SecurityLayer = httpx.SecurityLayerAPI
	}
	if cfg.AllowSession == nil {
		allow := false
		if cfg.SecurityLayer == httpx.SecurityLayerWeb {
			allow = true
		}
		cfg.AllowSession = &allow
	}
	if !strings.HasPrefix(cfg.BasePath, "/") {
		cfg.BasePath = "/" + cfg.BasePath
	}
	if cfg.Container == nil {
		cfg.Container = dibasic.New()
	}
	if cfg.AuthzRegistry == nil {
		cfg.AuthzRegistry = auth.NewRegistry()
	}
	if cfg.ModuleRegistry == nil {
		cfg.ModuleRegistry = NewModuleRegistry()
	}
	if cfg.MetadataRegistry == nil {
		cfg.MetadataRegistry = deventsourced.NewMetadataRegistry()
	}
	if cfg.ModuleHTTP == nil {
		cfg.ModuleHTTP = map[string]ModuleHTTPConfig{}
	}
	return cfg
}
