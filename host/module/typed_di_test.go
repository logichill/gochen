package module_test

import (
	"context"
	dibasic "gochen/di/basic"
	. "gochen/host/module"
	moduleasm "gochen/host/module/assembly"
	"gochen/host/module/runtimecap"
	"reflect"
	"testing"

	"gochen/di"
)

type typedProtocolTestContainer struct {
	inner          di.IContainer
	forbiddenCalls []string
}

func newTypedProtocolTestContainer() *typedProtocolTestContainer {
	return &typedProtocolTestContainer{inner: dibasic.New()}
}

func (c *typedProtocolTestContainer) recordForbiddenCall(name string) {
	c.forbiddenCalls = append(c.forbiddenCalls, name)
}

func (c *typedProtocolTestContainer) RegisterSingleton(serviceType reflect.Type, factory di.Factory) error {
	return c.inner.RegisterSingleton(serviceType, factory)
}

func (c *typedProtocolTestContainer) RegisterTransient(serviceType reflect.Type, factory di.Factory) error {
	return c.inner.RegisterTransient(serviceType, factory)
}

func (c *typedProtocolTestContainer) RegisterInstance(serviceType reflect.Type, instance di.Instance) error {
	return c.inner.RegisterInstance(serviceType, instance)
}

func (c *typedProtocolTestContainer) RegisterConstructor(constructor di.Constructor) error {
	c.recordForbiddenCall("RegisterConstructor")
	return c.inner.RegisterConstructor(constructor)
}

func (c *typedProtocolTestContainer) Resolve(serviceType reflect.Type) (any, error) {
	return c.inner.Resolve(serviceType)
}

func (c *typedProtocolTestContainer) IsRegistered(serviceType reflect.Type) bool {
	return c.inner.IsRegistered(serviceType)
}

func (c *typedProtocolTestContainer) RegisteredTypes() map[string]reflect.Type {
	return c.inner.RegisteredTypes()
}

func (c *typedProtocolTestContainer) Invoke(invocation di.Invocation) error {
	return c.inner.Invoke(invocation)
}

func (c *typedProtocolTestContainer) Clear() {
	c.inner.Clear()
}

func TestExplicitModule_UsesTypedDIProtocolAcrossInitAndRoutes(t *testing.T) {
	container := newTypedProtocolTestContainer()
	routeGroup := newMockRouteGroup()

	module := moduleasm.NewModule(moduleasm.ModuleDescriptor{
		ID:   "typed-di",
		Name: "Typed DI",
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
		NewModuleInitOptions(container, container, container),
		runtimecap.NewModuleHTTPOptions(routeGroup, ""),
	)

	if err := module.Init(opts); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	routeModule, ok := module.(IRouteModule)
	if !ok {
		t.Fatal("module does not implement IRouteModule")
	}
	if err := routeModule.RegisterRoutes(context.Background()); err != nil {
		t.Fatalf("RegisterRoutes failed: %v", err)
	}

	stop, err := module.Start(context.Background())
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if stop == nil {
		t.Fatal("expected stop function")
	}
	if err := stop(context.Background()); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	if len(container.forbiddenCalls) > 0 {
		t.Fatalf("expected typed DI protocol only, got forbidden calls: %v", container.forbiddenCalls)
	}
	if len(routeGroup.subGroups) == 0 {
		t.Fatal("expected route subgroup to be created")
	}
}
