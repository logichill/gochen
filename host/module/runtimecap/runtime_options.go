package runtimecap

import (
	"sync"

	auth "gochen/auth"
	deventsourced "gochen/domain/eventsourced"
	"gochen/host/capability"
	"gochen/host/module"
	initcap "gochen/host/module/initcap"
	"gochen/httpx"
)

var (
	httpOptionsKey       = initcap.NewKey[*ModuleHTTPOptions]("host.module.runtimecap.http")
	authzRegistryKey     = initcap.NewKey[*auth.Registry]("host.module.runtimecap.authz_registry")
	moduleRegistryKey    = initcap.NewKey[*module.ModuleRegistry]("host.module.runtimecap.module_registry")
	metadataRegistryKey  = initcap.NewKey[*deventsourced.MetadataRegistry]("host.module.runtimecap.metadata_registry")
	eventBusKey          = initcap.NewKey[capability.IEventSubscriber]("host.module.runtimecap.event_bus")
	projectionManagerKey = initcap.NewKey[capability.IProjectionManager]("host.module.runtimecap.projection_manager")
	transportKey         = initcap.NewKey[capability.ITransport]("host.module.runtimecap.transport")
)

// ModuleHTTPOptions 定义模块的 HTTP 挂载选项。
//
// 约定：
// - BaseGroup 由 Host 创建并注入（包含 BasePath 与全局中间件）；
// - 模块可基于 BaseGroup 派生自己的子 group（Prefix + Middlewares）；
// - 若 BaseGroup 为空，视为未启用 HTTP。
type ModuleHTTPOptions struct {
	baseGroup   httpx.IRouteGroup
	prefix      string
	middlewares []httpx.Middleware

	mountOnce  sync.Once
	mountGroup httpx.IRouteGroup
}

// NewModuleHTTPOptions 创建模块 HTTP 挂载选项。
func NewModuleHTTPOptions(baseGroup httpx.IRouteGroup, prefix string, middlewares ...httpx.Middleware) *ModuleHTTPOptions {
	return &ModuleHTTPOptions{
		baseGroup:   baseGroup,
		prefix:      prefix,
		middlewares: append([]httpx.Middleware(nil), middlewares...),
	}
}

// Prefix 返回模块挂载前缀。
func (o *ModuleHTTPOptions) Prefix() string {
	if o == nil {
		return ""
	}
	return o.prefix
}

// Middlewares 返回模块级 HTTP 中间件副本。
func (o *ModuleHTTPOptions) Middlewares() []httpx.Middleware {
	if o == nil || len(o.middlewares) == 0 {
		return nil
	}
	return append([]httpx.Middleware(nil), o.middlewares...)
}

// MountGroup 返回“模块实际应挂载的 group”。
func (o *ModuleHTTPOptions) MountGroup() httpx.IRouteGroup {
	if o == nil || o.baseGroup == nil {
		return nil
	}
	o.mountOnce.Do(func() {
		g := o.baseGroup
		if o.prefix == "" || o.prefix == "/" {
			g = g.Group("")
		} else {
			g = g.Group(o.prefix)
		}
		if len(o.middlewares) > 0 {
			g.Use(o.middlewares...)
		}
		o.mountGroup = g
	})
	return o.mountGroup
}

// HTTP 创建 HTTP capability setter。
func HTTP(http *ModuleHTTPOptions) initcap.Setter {
	return initcap.Set(httpOptionsKey, http)
}

// WithHTTP 注入模块 HTTP 挂载能力。
func WithHTTP(opts module.ModuleInitOptions, http *ModuleHTTPOptions) module.ModuleInitOptions {
	return module.WithCapability(opts, httpOptionsKey, http)
}

// HTTPFrom 读取模块 HTTP 挂载能力。
func HTTPFrom(opts module.ModuleInitOptions) *ModuleHTTPOptions {
	http, _ := module.CapabilityFrom(opts, httpOptionsKey)
	return http
}

// AuthzRegistry 创建授权目录 capability setter。
func AuthzRegistry(registry *auth.Registry) initcap.Setter {
	return initcap.Set(authzRegistryKey, registry)
}

// WithAuthzRegistry 注入模块授权目录能力。
func WithAuthzRegistry(opts module.ModuleInitOptions, registry *auth.Registry) module.ModuleInitOptions {
	return module.WithCapability(opts, authzRegistryKey, registry)
}

// AuthzRegistryFrom 读取模块授权目录能力。
func AuthzRegistryFrom(opts module.ModuleInitOptions) *auth.Registry {
	registry, _ := module.CapabilityFrom(opts, authzRegistryKey)
	return registry
}

// ModuleRegistry 创建模块目录 capability setter。
func ModuleRegistry(registry *module.ModuleRegistry) initcap.Setter {
	return initcap.Set(moduleRegistryKey, registry)
}

// WithModuleRegistry 注入模块目录能力。
func WithModuleRegistry(opts module.ModuleInitOptions, registry *module.ModuleRegistry) module.ModuleInitOptions {
	return module.WithCapability(opts, moduleRegistryKey, registry)
}

// ModuleRegistryFrom 读取模块目录能力。
func ModuleRegistryFrom(opts module.ModuleInitOptions) *module.ModuleRegistry {
	registry, _ := module.CapabilityFrom(opts, moduleRegistryKey)
	return registry
}

// MetadataRegistry 创建事件溯源聚合 metadata registry capability setter。
func MetadataRegistry(registry *deventsourced.MetadataRegistry) initcap.Setter {
	return initcap.Set(metadataRegistryKey, registry)
}

// WithMetadataRegistry 注入事件溯源聚合 metadata registry 能力。
func WithMetadataRegistry(opts module.ModuleInitOptions, registry *deventsourced.MetadataRegistry) module.ModuleInitOptions {
	return module.WithCapability(opts, metadataRegistryKey, registry)
}

// MetadataRegistryFrom 读取事件溯源聚合 metadata registry。
func MetadataRegistryFrom(opts module.ModuleInitOptions) *deventsourced.MetadataRegistry {
	registry, _ := module.CapabilityFrom(opts, metadataRegistryKey)
	return registry
}

// EventBus 创建事件订阅 capability setter。
func EventBus(eventBus capability.IEventSubscriber) initcap.Setter {
	return initcap.Set(eventBusKey, eventBus)
}

// WithEventBus 注入模块事件订阅能力。
func WithEventBus(opts module.ModuleInitOptions, eventBus capability.IEventSubscriber) module.ModuleInitOptions {
	return module.WithCapability(opts, eventBusKey, eventBus)
}

// EventBusFrom 读取模块事件订阅能力。
func EventBusFrom(opts module.ModuleInitOptions) capability.IEventSubscriber {
	eventBus, _ := module.CapabilityFrom(opts, eventBusKey)
	return eventBus
}

// ProjectionManager 创建投影注册 capability setter。
func ProjectionManager(manager capability.IProjectionManager) initcap.Setter {
	return initcap.Set(projectionManagerKey, manager)
}

// WithProjectionManager 注入模块投影注册能力。
func WithProjectionManager(opts module.ModuleInitOptions, manager capability.IProjectionManager) module.ModuleInitOptions {
	return module.WithCapability(opts, projectionManagerKey, manager)
}

// ProjectionManagerFrom 读取模块投影注册能力。
func ProjectionManagerFrom(opts module.ModuleInitOptions) capability.IProjectionManager {
	manager, _ := module.CapabilityFrom(opts, projectionManagerKey)
	return manager
}

// Transport 创建消息传输 capability setter。
func Transport(transport capability.ITransport) initcap.Setter {
	return initcap.Set(transportKey, transport)
}

// WithTransport 注入模块消息传输能力。
func WithTransport(opts module.ModuleInitOptions, transport capability.ITransport) module.ModuleInitOptions {
	return module.WithCapability(opts, transportKey, transport)
}

// TransportFrom 读取模块消息传输能力。
func TransportFrom(opts module.ModuleInitOptions) capability.ITransport {
	transport, _ := module.CapabilityFrom(opts, transportKey)
	return transport
}
