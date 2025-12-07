package integration

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gochen/eventing"
	"gochen/messaging"
	"gochen/messaging/bridge"
	"gochen/messaging/command"
)

func TestToEvent_ReturnsOriginal(t *testing.T) {
	evt := eventing.NewEvent[int64](1, "Order", "OrderCreated", 1, map[string]any{"k": "v"})

	out, err := ToEvent(evt)
	require.NoError(t, err)
	assert.Same(t, evt, out)
}

func TestToEvent_FromBridgeMessageWithRaw(t *testing.T) {
	serializer := bridge.NewJSONSerializer()
	orig := eventing.NewEvent[int64](123, "Order", "OrderCreated", 3, map[string]any{"foo": "bar"}, 4)

	data, err := serializer.SerializeEvent(orig)
	require.NoError(t, err)

	msg, err := serializer.DeserializeEvent(data)
	require.NoError(t, err)

	out, err := ToEvent(msg)
	require.NoError(t, err)

	typed, ok := out.(*eventing.Event[int64])
	require.True(t, ok)

	assert.Equal(t, orig.AggregateID, typed.GetAggregateID())
	assert.Equal(t, orig.AggregateType, typed.GetAggregateType())
	assert.Equal(t, orig.GetType(), typed.GetType())
	assert.Equal(t, orig.GetVersion(), typed.GetVersion())
	assert.Equal(t, orig.GetSchemaVersion(), typed.GetSchemaVersion())
	assert.Equal(t, orig.GetPayload(), typed.GetPayload())
}

func TestRegisterBridgeEventHandler_WithAdapter(t *testing.T) {
	br := &stubBridge{}
	handler := &mockEventHandler{}

	err := RegisterBridgeEventHandler(br, "OrderCreated", handler)
	require.NoError(t, err)
	require.NotNil(t, br.lastEventHandler)

	serializer := bridge.NewJSONSerializer()
	evt := eventing.NewEvent[int64](10, "Order", "OrderCreated", 1, map[string]any{"x": 1})
	data, err := serializer.SerializeEvent(evt)
	require.NoError(t, err)
	msg, err := serializer.DeserializeEvent(data)
	require.NoError(t, err)

	err = br.lastEventHandler.Handle(context.Background(), msg)
	require.NoError(t, err)

	assert.True(t, handler.called)
	assert.Equal(t, evt.GetID(), handler.lastEvent.GetID())
	typed, ok := handler.lastEvent.(*eventing.Event[int64])
	require.True(t, ok)
	assert.Equal(t, evt.AggregateID, typed.GetAggregateID())
}

type stubBridge struct {
	lastEventType    string
	lastEventHandler messaging.IMessageHandler
}

func (s *stubBridge) SendCommand(ctx context.Context, serviceURL string, cmd *command.Command) error {
	return nil
}

func (s *stubBridge) SendEvent(ctx context.Context, serviceURL string, event messaging.IMessage) error {
	return nil
}

func (s *stubBridge) RegisterCommandHandler(commandType string, handler func(ctx context.Context, cmd *command.Command) error) error {
	return nil
}

func (s *stubBridge) RegisterEventHandler(eventType string, handler messaging.IMessageHandler) error {
	s.lastEventType = eventType
	s.lastEventHandler = handler
	return nil
}

func (s *stubBridge) Start() error { return nil }
func (s *stubBridge) Stop() error  { return nil }

type mockEventHandler struct {
	called    bool
	lastEvent eventing.IEvent
}

func (m *mockEventHandler) HandleEvent(ctx context.Context, evt eventing.IEvent) error {
	m.called = true
	m.lastEvent = evt
	return nil
}

func (m *mockEventHandler) Handle(ctx context.Context, msg messaging.IMessage) error {
	evt, ok := msg.(eventing.IEvent)
	if !ok {
		return fmt.Errorf("not event")
	}
	return m.HandleEvent(ctx, evt)
}

func (m *mockEventHandler) GetEventTypes() []string { return []string{"*"} }
func (m *mockEventHandler) GetHandlerName() string  { return "mock" }
func (m *mockEventHandler) Type() string            { return "mock" }
