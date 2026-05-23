package bootstrap

import (
	"context"
	dibasic "gochen/di/basic"
	"reflect"
	"testing"

	"gochen/di"
	"gochen/eventing/bus"
	"gochen/eventing/projection"
	"gochen/messaging"
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

type mockTransport struct{}

func (m *mockTransport) Publish(context.Context, messaging.IMessage) error      { return nil }
func (m *mockTransport) PublishAll(context.Context, []messaging.IMessage) error { return nil }
func (m *mockTransport) Subscribe(context.Context, string, messaging.IMessageHandler) (messaging.UnsubscribeFunc, error) {
	return func(context.Context) error { return nil }, nil
}
func (m *mockTransport) Start(context.Context) error     { return nil }
func (m *mockTransport) Stop(context.Context) error      { return nil }
func (m *mockTransport) Stats() messaging.TransportStats { return messaging.TransportStats{} }

func TestAssemblyHelpers_UseTypedDIProtocol(t *testing.T) {
	container := newTypedProtocolTestContainer()
	transport := &mockTransport{}
	eventBus := bus.NewEventBus(messaging.NewMessageBus(transport))
	pm := &projection.ProjectionManager[int64]{}

	if err := container.RegisterInstance(reflect.TypeOf((*messaging.ITransport)(nil)).Elem(), di.NewInstance(transport)); err != nil {
		t.Fatalf("register transport failed: %v", err)
	}
	if err := container.RegisterInstance(reflect.TypeOf((*bus.IEventBus)(nil)).Elem(), di.NewInstance(eventBus)); err != nil {
		t.Fatalf("register event bus failed: %v", err)
	}
	if err := container.RegisterInstance(reflect.TypeOf((*projection.IProjectionRegistrar)(nil)).Elem(), di.NewInstance(pm)); err != nil {
		t.Fatalf("register projection registrar failed: %v", err)
	}

	gotTransport, ok := tryResolveTransport(container)
	if !ok || gotTransport != transport {
		t.Fatalf("unexpected transport: %#v", gotTransport)
	}

	gotEventBus, ok := tryResolveEventBus(container)
	if !ok || gotEventBus != eventBus {
		t.Fatalf("unexpected event bus: %#v", gotEventBus)
	}

	gotPM, err := resolveProjectionManager(nil, container)
	if err != nil {
		t.Fatalf("resolve projection manager failed: %v", err)
	}
	if gotPM != pm {
		t.Fatalf("unexpected projection manager: %#v", gotPM)
	}

	other := newTypedProtocolTestContainer()
	otherTransport := &mockTransport{}
	if err := registerInstanceByType(other, (*messaging.ITransport)(nil), otherTransport); err != nil {
		t.Fatalf("registerInstanceByType failed: %v", err)
	}
	if !other.IsRegistered(reflect.TypeOf((*messaging.ITransport)(nil)).Elem()) {
		t.Fatal("expected transport to be registered by type")
	}

	if len(container.forbiddenCalls) > 0 {
		t.Fatalf("expected typed DI protocol only, got forbidden calls: %v", container.forbiddenCalls)
	}
	if len(other.forbiddenCalls) > 0 {
		t.Fatalf("expected typed DI protocol only, got forbidden calls in helper path: %v", other.forbiddenCalls)
	}
}
