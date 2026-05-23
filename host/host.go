package host

import (
	"context"
	"time"

	auth "gochen/auth"
	"gochen/di"
	deventsourced "gochen/domain/eventsourced"
	"gochen/host/capability"
	"gochen/host/engine"
	"gochen/host/module"
	"gochen/host/module/runtime"
	"gochen/httpx"
	"gochen/logging"
)

// IModule 定义 Host 可管理的基础模块生命周期。
//
// 业务模块通常只需要在构造函数签名中返回该类型：
//
//	func NewModule() (host.IModule, error)
type IModule = module.IModule

// ModuleHTTPConfig 定义模块级 HTTP 挂载配置。
//
// 仅在组合根需要覆盖某个模块的路由前缀或中间件时使用。
type ModuleHTTPConfig = runtime.ModuleHTTPConfig

// Option 配置标准模块化 Host 启动入口。
type Option func(*options)

type options struct {
	modules       []module.ModuleCtor
	hostOptions   []runtime.Option
	engineOptions []engine.Option
}

// New 创建标准模块化 Host 的生命周期引擎。
//
// 常规项目优先使用 Run；只有调用方需要自行控制 Start/Stop 时才使用 New。
func New(opts ...Option) (*engine.Engine, error) {
	cfg := collectOptions(opts...)
	app := runtime.NewHost(cfg.modules, cfg.hostOptions...)
	return engine.NewEngine(app, cfg.engineOptions...)
}

// Run 创建标准模块化 Host 并执行完整生命周期。
func Run(ctx context.Context, opts ...Option) error {
	runner, err := New(opts...)
	if err != nil {
		return err
	}
	return runner.Start(ctx)
}

// NewEngine 创建自定义应用的生命周期引擎。
//
// Advanced: 模块化应用优先使用 Run/New；只有不采用 Host 模块模型的应用才需要直接使用该入口。
func NewEngine(app engine.IApp, opts ...engine.Option) (*engine.Engine, error) {
	return engine.NewEngine(app, opts...)
}

// WithModules 设置模块构造函数列表。
func WithModules(ctors ...module.ModuleCtor) Option {
	return func(o *options) {
		o.modules = append(o.modules, ctors...)
	}
}

// WithEngineOptions 追加生命周期引擎选项。
//
// Advanced: 常规项目优先使用 host.WithName/WithVersion/WithStartupTimeout 等根选项。
func WithEngineOptions(opts ...engine.Option) Option {
	return func(o *options) {
		o.engineOptions = append(o.engineOptions, opts...)
	}
}

// WithName 设置服务名称，同时作用于模块 Host 与生命周期引擎日志。
func WithName(name string) Option {
	return func(o *options) {
		o.hostOptions = append(o.hostOptions, runtime.WithName(name))
		o.engineOptions = append(o.engineOptions, engine.WithName(name))
	}
}

// WithVersion 设置服务版本号。
func WithVersion(version string) Option {
	return func(o *options) {
		o.engineOptions = append(o.engineOptions, engine.WithVersion(version))
	}
}

// WithStartupTimeout 设置启动阶段超时。
func WithStartupTimeout(timeout time.Duration) Option {
	return func(o *options) {
		o.engineOptions = append(o.engineOptions, engine.WithStartupTimeout(timeout))
	}
}

// WithShutdownTimeout 设置优雅关闭阶段超时。
func WithShutdownTimeout(timeout time.Duration) Option {
	return func(o *options) {
		o.engineOptions = append(o.engineOptions, engine.WithShutdownTimeout(timeout))
	}
}

// WithLogger 设置生命周期引擎日志实现。
func WithLogger(logger logging.ILogger) Option {
	return func(o *options) {
		o.engineOptions = append(o.engineOptions, engine.WithLogger(logger))
	}
}

// WithBeforeInit 添加初始化前回调。
//
// Advanced: 用于组合根需要接入额外生命周期钩子的场景。
func WithBeforeInit(fn engine.Hook) Option {
	return func(o *options) {
		o.engineOptions = append(o.engineOptions, engine.WithBeforeInit(fn))
	}
}

// WithAfterInit 添加初始化后回调。
//
// Advanced: 用于组合根需要接入额外生命周期钩子的场景。
func WithAfterInit(fn engine.Hook) Option {
	return func(o *options) {
		o.engineOptions = append(o.engineOptions, engine.WithAfterInit(fn))
	}
}

// WithBeforeStart 添加启动前回调。
//
// Advanced: 用于组合根需要接入额外生命周期钩子的场景。
func WithBeforeStart(fn engine.Hook) Option {
	return func(o *options) {
		o.engineOptions = append(o.engineOptions, engine.WithBeforeStart(fn))
	}
}

// WithAfterStart 添加启动后回调。
//
// Advanced: 用于组合根需要接入额外生命周期钩子的场景。
func WithAfterStart(fn engine.Hook) Option {
	return func(o *options) {
		o.engineOptions = append(o.engineOptions, engine.WithAfterStart(fn))
	}
}

// WithBeforeStop 添加停止前回调。
//
// Advanced: 用于组合根需要接入额外生命周期钩子的场景。
func WithBeforeStop(fn engine.Hook) Option {
	return func(o *options) {
		o.engineOptions = append(o.engineOptions, engine.WithBeforeStop(fn))
	}
}

// WithAfterStop 添加停止后回调。
//
// Advanced: 用于组合根需要接入额外生命周期钩子的场景。
func WithAfterStop(fn engine.Hook) Option {
	return func(o *options) {
		o.engineOptions = append(o.engineOptions, engine.WithAfterStop(fn))
	}
}

// WithAddress 设置 HTTP 监听地址。
func WithAddress(host string) Option {
	return func(o *options) {
		o.hostOptions = append(o.hostOptions, runtime.WithAddress(host))
	}
}

// WithPort 设置 HTTP 监听端口。
func WithPort(port int) Option {
	return func(o *options) {
		o.hostOptions = append(o.hostOptions, runtime.WithPort(port))
	}
}

// WithBasePath 设置模块路由基础前缀。
func WithBasePath(basePath string) Option {
	return func(o *options) {
		o.hostOptions = append(o.hostOptions, runtime.WithBasePath(basePath))
	}
}

// WithContainer 注入自定义 DI 容器。
func WithContainer(container di.IContainer) Option {
	return func(o *options) {
		o.hostOptions = append(o.hostOptions, runtime.WithContainer(container))
	}
}

// WithEventBus 注入自定义事件总线。
func WithEventBus(eventBus capability.IEventSubscriber) Option {
	return func(o *options) {
		o.hostOptions = append(o.hostOptions, runtime.WithEventBus(eventBus))
	}
}

// WithTransport 注入消息传输层。
func WithTransport(transport capability.ITransport) Option {
	return func(o *options) {
		o.hostOptions = append(o.hostOptions, runtime.WithTransport(transport))
	}
}

// WithProjectionManager 注入投影管理器。
func WithProjectionManager(manager capability.IProjectionManager) Option {
	return func(o *options) {
		o.hostOptions = append(o.hostOptions, runtime.WithProjectionManager(manager))
	}
}

// WithAuthzRegistry 注入模块级 authz 注册表。
func WithAuthzRegistry(registry *auth.Registry) Option {
	return func(o *options) {
		o.hostOptions = append(o.hostOptions, runtime.WithAuthzRegistry(registry))
	}
}

// WithModuleRegistry 注入模块目录注册表。
func WithModuleRegistry(registry *module.ModuleRegistry) Option {
	return func(o *options) {
		o.hostOptions = append(o.hostOptions, runtime.WithModuleRegistry(registry))
	}
}

// WithMetadataRegistry 注入事件溯源聚合 metadata 注册表。
func WithMetadataRegistry(registry *deventsourced.MetadataRegistry) Option {
	return func(o *options) {
		o.hostOptions = append(o.hostOptions, runtime.WithMetadataRegistry(registry))
	}
}

// WithHTTPServer 注入自定义 HTTP server。
func WithHTTPServer(server httpx.IServer) Option {
	return func(o *options) {
		o.hostOptions = append(o.hostOptions, runtime.WithHTTPServer(server))
	}
}

// WithRouteMiddlewares 追加挂载在 BasePath group 上的统一中间件。
func WithRouteMiddlewares(middlewares ...httpx.Middleware) Option {
	return func(o *options) {
		o.hostOptions = append(o.hostOptions, runtime.WithRouteMiddlewares(middlewares...))
	}
}

// WithDisableHealthRoute 控制是否跳过框架默认健康检查路由。
func WithDisableHealthRoute(disable bool) Option {
	return func(o *options) {
		o.hostOptions = append(o.hostOptions, runtime.WithDisableHealthRoute(disable))
	}
}

// WithFailFastOnRouteConflicts 在启动期检测重复路由并直接失败。
func WithFailFastOnRouteConflicts(enabled bool) Option {
	return func(o *options) {
		o.hostOptions = append(o.hostOptions, runtime.WithFailFastOnRouteConflicts(enabled))
	}
}

// WithModuleHTTP 设置指定模块的 HTTP 挂载配置。
func WithModuleHTTP(moduleID string, cfg ModuleHTTPConfig) Option {
	return func(o *options) {
		o.hostOptions = append(o.hostOptions, runtime.WithModuleHTTP(moduleID, cfg))
	}
}

// WithSecurityLayer 显式指定服务默认采用的安全分层。
func WithSecurityLayer(layer httpx.SecurityLayer) Option {
	return func(o *options) {
		o.hostOptions = append(o.hostOptions, runtime.WithSecurityLayer(layer))
	}
}

// WithAllowSession 控制 API 安全层是否允许携带会话语义。
func WithAllowSession(allow bool) Option {
	return func(o *options) {
		o.hostOptions = append(o.hostOptions, runtime.WithAllowSession(allow))
	}
}

// NewModuleRegistry 创建模块目录注册表。
//
// Advanced: 仅在组合根需要读取 Host 注册后的模块目录时使用。
func NewModuleRegistry() *module.ModuleRegistry {
	return module.NewModuleRegistry()
}

// NewMetadataRegistry 创建事件溯源聚合 metadata 注册表。
func NewMetadataRegistry() *deventsourced.MetadataRegistry {
	return deventsourced.NewMetadataRegistry()
}

func collectOptions(opts ...Option) options {
	var cfg options
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return cfg
}
