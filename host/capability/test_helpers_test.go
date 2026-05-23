package capability

import (
	"context"

	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/projection"
	"gochen/messaging"
)

type mockTransport struct {
	subscriptions map[string][]messaging.IMessageHandler
	startCalls    int
	log           *[]string
}

func newMockTransport() *mockTransport {
	return &mockTransport{subscriptions: make(map[string][]messaging.IMessageHandler)}
}

func (t *mockTransport) Publish(context.Context, messaging.IMessage) error      { return nil }
func (t *mockTransport) PublishAll(context.Context, []messaging.IMessage) error { return nil }
func (t *mockTransport) Subscribe(ctx context.Context, messageType string, handler messaging.IMessageHandler) (messaging.UnsubscribeFunc, error) {
	t.subscriptions[messageType] = append(t.subscriptions[messageType], handler)
	if t.log != nil {
		*t.log = append(*t.log, "subscribe:"+messageType)
	}
	return func(context.Context) error {
		if t.log != nil {
			*t.log = append(*t.log, "unsubscribe:"+messageType)
		}
		return nil
	}, nil
}
func (t *mockTransport) Start(context.Context) error { t.startCalls++; return nil }
func (t *mockTransport) Stop(context.Context) error  { return nil }
func (t *mockTransport) Stats() messaging.TransportStats {
	return messaging.TransportStats{}
}

type testEventHandler struct {
	msgType string
}

func newTestEventHandler() *testEventHandler {
	return &testEventHandler{msgType: "test.event"}
}

func (h *testEventHandler) Type() string { return h.msgType }
func (h *testEventHandler) Handle(context.Context, messaging.IMessage) error {
	return nil
}

type multiTypeEventHandler struct {
	msgTypes []string
}

func newMultiTypeEventHandler() *multiTypeEventHandler {
	return &multiTypeEventHandler{msgTypes: []string{"a.event", "b.event"}}
}

func (h *multiTypeEventHandler) Type() string { return "multiTypeEventHandler" }
func (h *multiTypeEventHandler) Handle(context.Context, messaging.IMessage) error {
	return nil
}
func (h *multiTypeEventHandler) EventTypes() []string { return h.msgTypes }

type emptyTypeEventHandler struct{}

func (h *emptyTypeEventHandler) Type() string { return "" }
func (h *emptyTypeEventHandler) Handle(context.Context, messaging.IMessage) error {
	return nil
}

type testProjectionBase struct{ name string }

func (p *testProjectionBase) Name() string                                           { return p.name }
func (p *testProjectionBase) Handle(context.Context, eventing.IEvent) error          { return nil }
func (p *testProjectionBase) SupportedEventTypes() []string                          { return []string{"*"} }
func (p *testProjectionBase) Rebuild(context.Context, []eventing.Event[int64]) error { return nil }
func (p *testProjectionBase) Status() projection.ProjectionStatus {
	return projection.ProjectionStatus{Name: p.name, Status: "stopped"}
}

type testProjection1 struct{ testProjectionBase }
type testProjection2 struct{ testProjectionBase }

func newTestProjection1() *testProjection1 {
	return &testProjection1{testProjectionBase: testProjectionBase{name: "p1"}}
}

func newTestProjection2() *testProjection2 {
	return &testProjection2{testProjectionBase: testProjectionBase{name: "p2"}}
}

type mockProjectionManager struct {
	registered []string
	started    []string
	stopped    []string
	log        *[]string
}

func (m *mockProjectionManager) RegisterProjectionAny(ctx context.Context, p any) error {
	projection, ok := p.(projection.IProjection[int64])
	if !ok {
		return errors.NewCode(errors.InvalidInput, "projection type mismatch")
	}
	return m.RegisterProjectionWithContext(ctx, projection)
}

func (m *mockProjectionManager) RegisterProjectionWithContext(_ context.Context, p projection.IProjection[int64]) error {
	name := p.Name()
	m.registered = append(m.registered, name)
	if m.log != nil {
		*m.log = append(*m.log, "projection:register:"+name)
	}
	return nil
}

func (m *mockProjectionManager) StartProjection(name string) error {
	m.started = append(m.started, name)
	if m.log != nil {
		*m.log = append(*m.log, "projection:start:"+name)
	}
	return nil
}

func (m *mockProjectionManager) StopProjection(name string) error {
	m.stopped = append(m.stopped, name)
	if m.log != nil {
		*m.log = append(*m.log, "projection:stop:"+name)
	}
	return nil
}

type testRuntimeComponent struct {
	startedWith string
	startCount  int
	stopCount   int
	log         *[]string
}

func (c *testRuntimeComponent) Start(context.Context) error {
	c.startCount++
	if c.log != nil {
		*c.log = append(*c.log, "runtime:start")
	}
	if c.startedWith == "" {
		c.startedWith = "test"
	}
	return nil
}

func (c *testRuntimeComponent) Stop(context.Context) error {
	c.stopCount++
	if c.log != nil {
		*c.log = append(*c.log, "runtime:stop")
	}
	return nil
}
