package messaging

import (
	"context"
	"errors"
	"testing"
)

type mockTransport struct {
	published     []IMessage
	batch         [][]IMessage
	subscribed    map[string]int
	unsubscribed  map[string]int
	shouldError   error
	orderRecorder *[]string
}

func newMockTransport() *mockTransport {
	return &mockTransport{
		published:    make([]IMessage, 0),
		batch:        make([][]IMessage, 0),
		subscribed:   make(map[string]int),
		unsubscribed: make(map[string]int),
	}
}

func (m *mockTransport) Publish(ctx context.Context, message IMessage) error {
	if m.orderRecorder != nil {
		*m.orderRecorder = append(*m.orderRecorder, "transport")
	}
	m.published = append(m.published, message)
	return m.shouldError
}

func (m *mockTransport) PublishAll(ctx context.Context, messages []IMessage) error {
	m.batch = append(m.batch, messages)
	return m.shouldError
}

func (m *mockTransport) Subscribe(messageType string, handler IMessageHandler) error {
	m.subscribed[messageType]++
	return nil
}

func (m *mockTransport) Unsubscribe(messageType string, handler IMessageHandler) error {
	m.unsubscribed[messageType]++
	return nil
}

func (m *mockTransport) Start(ctx context.Context) error { return nil }

func (m *mockTransport) Close() error { return nil }

func (m *mockTransport) Stats() TransportStats { return TransportStats{} }

type recordingMiddleware struct {
	name  string
	order *[]string
	err   error
}

func (mw recordingMiddleware) Handle(ctx context.Context, message IMessage, next HandlerFunc) error {
	*mw.order = append(*mw.order, mw.name)
	if mw.err != nil {
		return mw.err
	}
	return next(ctx, message)
}

func (mw recordingMiddleware) Name() string {
	return mw.name
}

type noopHandler struct{}

func (noopHandler) Handle(ctx context.Context, message IMessage) error { return nil }
func (noopHandler) Type() string                                       { return "noop" }

func TestMessageBus_PublishWithMiddleware(t *testing.T) {
	order := make([]string, 0, 3)
	transport := newMockTransport()
	transport.orderRecorder = &order

	bus := NewMessageBus(transport)
	bus.Use(recordingMiddleware{name: "mw1", order: &order})
	bus.Use(recordingMiddleware{name: "mw2", order: &order})

	msg := &Message{ID: "msg-1", Type: "test"}

	if err := bus.Publish(context.Background(), msg); err != nil {
		t.Fatalf("publish failed: %v", err)
	}

	expectedOrder := []string{"mw1", "mw2", "transport"}
	if len(order) != len(expectedOrder) {
		t.Fatalf("unexpected order length: got %v want %v", order, expectedOrder)
	}
	for i, v := range expectedOrder {
		if order[i] != v {
			t.Fatalf("unexpected order at %d: got %s want %s", i, order[i], v)
		}
	}

	if len(transport.published) != 1 || transport.published[0] != msg {
		t.Fatalf("expected message to be published once")
	}
}

func TestMessageBus_PublishAllMiddlewareError(t *testing.T) {
	order := make([]string, 0, 1)
	transport := newMockTransport()

	mwErr := errors.New("middleware failed")
	bus := NewMessageBus(transport)
	bus.Use(recordingMiddleware{
		name:  "mw-error",
		order: &order,
		err:   mwErr,
	})

	msg := &Message{ID: "msg-err", Type: "test"}
	err := bus.PublishAll(context.Background(), []IMessage{msg})
	if err == nil {
		t.Fatalf("expected error from PublishAll")
	}
	if !errors.Is(err, mwErr) {
		t.Fatalf("expected middleware error to be wrapped, got %v", err)
	}
	if len(transport.batch) != 0 {
		t.Fatalf("expected batch not to be published on error")
	}
	if len(order) != 1 || order[0] != "mw-error" {
		t.Fatalf("middleware order not recorded: %v", order)
	}
}

func TestMessageBus_PublishAllSuccess(t *testing.T) {
	transport := newMockTransport()
	bus := NewMessageBus(transport)

	msg1 := &Message{ID: "msg-1", Type: "t"}
	msg2 := &Message{ID: "msg-2", Type: "t"}

	if err := bus.PublishAll(context.Background(), []IMessage{msg1, msg2}); err != nil {
		t.Fatalf("publish all failed: %v", err)
	}

	if len(transport.batch) != 1 {
		t.Fatalf("expected one batch, got %d", len(transport.batch))
	}

	batch := transport.batch[0]
	if len(batch) != 2 || batch[0] != msg1 || batch[1] != msg2 {
		t.Fatalf("unexpected batch content: %+v", batch)
	}
}

func TestMessageBus_SubscribeDelegation(t *testing.T) {
	transport := newMockTransport()
	bus := NewMessageBus(transport)

	handler := noopHandler{}
	if err := bus.Subscribe(context.Background(), "test", handler); err != nil {
		t.Fatalf("subscribe failed: %v", err)
	}
	if transport.subscribed["test"] != 1 {
		t.Fatalf("expected subscription to be delegated")
	}

	if err := bus.Unsubscribe(context.Background(), "test", handler); err != nil {
		t.Fatalf("unsubscribe failed: %v", err)
	}
	if transport.unsubscribed["test"] != 1 {
		t.Fatalf("expected unsubscribe to be delegated")
	}
}
