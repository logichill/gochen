package bootstrap

import (
	dibasic "gochen/di/basic"
	"net/http"
	"reflect"
	"strings"

	"gochen/di"
	"gochen/errors"
	"gochen/eventing/bus"
	"gochen/eventing/monitoring"
	"gochen/eventing/projection"
	"gochen/host/capability"
	"gochen/host/internal/runtimeutil"
	"gochen/httpx"
	hmw "gochen/httpx/middleware"
	"gochen/httpx/nethttp"
	"gochen/messaging"
)

// Config 描述 Host 运行期默认装配所需的外围组件。
type Config struct {
	Container di.IContainer

	Host     string
	Port     int
	BasePath string

	SecurityLayer            httpx.SecurityLayer
	AllowSession             *bool
	RouteMiddlewares         []httpx.Middleware
	DisableHealthRoute       bool
	FailFastOnRouteConflicts bool

	HTTPServer        httpx.IServer
	EventBus          capability.IEventSubscriber
	Transport         capability.ITransport
	ProjectionManager capability.IProjectionManager
}

// Runtime 表示 Host 已就绪的外围运行时能力。
type Runtime struct {
	Container         di.IContainer
	HTTPServer        httpx.IServer
	BaseGroup         httpx.IRouteGroup
	EventBus          capability.IEventSubscriber
	Transport         capability.ITransport
	ProjectionManager capability.IProjectionManager
}

// Prepare 构造 Host 运行期需要的外围组件。
func Prepare(cfg Config) (*Runtime, error) {
	container := cfg.Container
	if container == nil {
		container = dibasic.New()
	}

	rt := &Runtime{Container: container}

	if err := prepareMessaging(rt, cfg); err != nil {
		return nil, err
	}
	if err := prepareHTTP(rt, cfg); err != nil {
		return nil, err
	}

	baseGroup, err := prepareBaseGroup(rt.HTTPServer, cfg)
	if err != nil {
		return nil, err
	}
	rt.BaseGroup = baseGroup

	pm, err := resolveProjectionManager(cfg.ProjectionManager, container)
	if err != nil {
		return nil, err
	}
	rt.ProjectionManager = pm

	return rt, nil
}

func prepareMessaging(rt *Runtime, cfg Config) error {
	if rt == nil || rt.Container == nil {
		return errors.NewCode(errors.Internal, "runtime container is nil")
	}

	if cfg.Transport != nil {
		rt.Transport = cfg.Transport
		if err := registerInstanceByType(rt.Container, (*messaging.ITransport)(nil), rt.Transport); err != nil {
			return err
		}
	} else if tr, ok := tryResolveTransport(rt.Container); ok {
		rt.Transport = tr
	}

	if cfg.EventBus != nil {
		rt.EventBus = cfg.EventBus
		if eb, ok := rt.EventBus.(bus.IEventBus); ok && !runtimeutil.IsTypedNil(eb) {
			if err := registerInstanceByType(rt.Container, (*bus.IEventBus)(nil), eb); err != nil {
				return err
			}
		}
	} else if eb, ok := tryResolveEventBus(rt.Container); ok {
		rt.EventBus = eb
	}

	return nil
}

func prepareHTTP(rt *Runtime, cfg Config) error {
	if rt == nil {
		return errors.NewCode(errors.Internal, "runtime is nil")
	}

	if cfg.HTTPServer != nil {
		rt.HTTPServer = cfg.HTTPServer
	} else {
		rt.HTTPServer = nethttp.NewServer(&httpx.WebConfig{
			Host:         cfg.Host,
			Port:         cfg.Port,
			ReadTimeout:  httpx.DefaultReadTimeout,
			WriteTimeout: httpx.DefaultWriteTimeout,
			IdleTimeout:  httpx.DefaultIdleTimeout,
		})
	}

	if rt.HTTPServer != nil && cfg.FailFastOnRouteConflicts {
		rt.HTTPServer = httpx.WithRouteRegistry(rt.HTTPServer)
	}

	return nil
}

func prepareBaseGroup(httpServer httpx.IServer, cfg Config) (httpx.IRouteGroup, error) {
	if httpServer == nil {
		return nil, nil
	}

	basePath := normalizeBasePath(cfg.BasePath)
	if basePath == "/" {
		basePath = ""
	}
	group := httpServer.Group(basePath)

	layer := cfg.SecurityLayer
	if layer == "" {
		layer = httpx.SecurityLayerAPI
	}
	allowSession := defaultAllowSessionForLayer(layer)
	if cfg.AllowSession != nil {
		allowSession = *cfg.AllowSession
	}

	baseMws := []httpx.Middleware{
		hmw.SecurityLayer(hmw.SecurityLayerConfig{
			Layer:        layer,
			AllowSession: allowSession,
		}),
	}
	if len(cfg.RouteMiddlewares) > 0 {
		baseMws = append(baseMws, cfg.RouteMiddlewares...)
	}
	group.Use(baseMws...)

	if !cfg.DisableHealthRoute {
		registerMonitoringRoutes(httpServer)
	}

	return group, nil
}

func registerMonitoringRoutes(httpServer httpx.IServer) {
	if httpServer == nil {
		return
	}

	reg := monitoring.DefaultRegistry()

	httpServer.GET("/healthz", func(ctx httpx.IContext) error {
		report := reg.Health.Report(ctx.RequestContext())
		code := http.StatusOK
		if report.Status == monitoring.HealthStatusUnhealthy {
			code = http.StatusServiceUnavailable
		}
		return ctx.JSON(code, httpx.JSONValue(report))
	})

	httpServer.GET("/readyz", func(ctx httpx.IContext) error {
		report := reg.Health.Report(ctx.RequestContext())
		code := http.StatusOK
		if report.Status == monitoring.HealthStatusUnhealthy {
			code = http.StatusServiceUnavailable
		}
		return ctx.JSON(code, httpx.JSONValue(report))
	})

	httpServer.GET("/metrics", func(ctx httpx.IContext) error {
		summary := reg.Metrics.Snapshot().Summary()
		return ctx.JSON(http.StatusOK, httpx.JSONValue(summary))
	})

	httpServer.GET("/snapshot", func(ctx httpx.IContext) error {
		snap := reg.Snapshot(ctx.RequestContext())
		code := http.StatusOK
		if snap.Health.Status == monitoring.HealthStatusUnhealthy {
			code = http.StatusServiceUnavailable
		}
		return ctx.JSON(code, httpx.JSONValue(snap))
	})
}

func normalizeBasePath(basePath string) string {
	if basePath == "" {
		return "/api/v1"
	}
	if !strings.HasPrefix(basePath, "/") {
		basePath = "/" + basePath
	}
	return basePath
}

func defaultAllowSessionForLayer(layer httpx.SecurityLayer) bool {
	switch layer {
	case httpx.SecurityLayerAPI:
		return false
	case httpx.SecurityLayerWeb:
		return true
	default:
		return false
	}
}

func tryResolveEventBus(container di.IResolver) (bus.IEventBus, bool) {
	if container == nil {
		return nil, false
	}
	inst, err := container.Resolve(reflect.TypeOf((*bus.IEventBus)(nil)).Elem())
	if err != nil {
		return nil, false
	}
	if eb, ok := inst.(bus.IEventBus); ok && !runtimeutil.IsTypedNil(eb) {
		return eb, true
	}
	return nil, false
}

func tryResolveTransport(container di.IResolver) (messaging.ITransport, bool) {
	if container == nil {
		return nil, false
	}
	inst, err := container.Resolve(reflect.TypeOf((*messaging.ITransport)(nil)).Elem())
	if err != nil {
		return nil, false
	}
	if tr, ok := inst.(messaging.ITransport); ok && !runtimeutil.IsTypedNil(tr) {
		return tr, true
	}
	return nil, false
}

func resolveProjectionManager(injected capability.IProjectionManager, container di.IResolver) (capability.IProjectionManager, error) {
	if injected != nil {
		return injected, nil
	}
	if container == nil {
		return nil, nil
	}

	pmIfaceType := reflect.TypeOf((*projection.IProjectionRegistrar)(nil)).Elem()
	if pmAny, err := container.Resolve(pmIfaceType); err == nil {
		pm, ok := pmAny.(capability.IProjectionManager)
		if ok && !runtimeutil.IsTypedNil(pm) {
			return pm, nil
		}
		return nil, errors.NewCode(errors.Internal, "resolved projection registrar has invalid type").
			WithContext("service_type", di.TypeKey(pmIfaceType)).
			WithContext("type", runtimeutil.TypeString(pmAny))
	}

	return nil, nil
}

func registerInstanceByType(container di.IContainer, ifacePtr any, instance any) error {
	if container == nil || ifacePtr == nil || instance == nil {
		return nil
	}

	t := reflect.TypeOf(ifacePtr)
	if t.Kind() != reflect.Ptr {
		return nil
	}
	iface := t.Elem()
	if container.IsRegistered(iface) {
		return nil
	}

	instValue := reflect.ValueOf(instance)
	if !instValue.IsValid() {
		return nil
	}
	instType := instValue.Type()
	if !instType.AssignableTo(iface) {
		return errors.NewCode(errors.InvalidInput, "instance type is not assignable to target type").
			WithContext("target_type", di.TypeKey(iface)).
			WithContext("instance_type", di.TypeKey(instType)).
			WithContext("instance_type_raw", instType.String())
	}
	return container.RegisterInstance(iface, di.NewInstance(instance))
}
