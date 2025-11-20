package messaging_test

import (
	"context"
	"testing"
	"time"

	"gochen/messaging"
	synctransport "gochen/messaging/transport/sync"
)

type testMessage struct {
	id   string
	typ  string
	data string
}

func (m *testMessage) GetID() string           { return m.id }
func (m *testMessage) GetType() string         { return m.typ }
func (m *testMessage) GetTimestamp() time.Time { return time.Now() }
func (m *testMessage) GetPayload() any         { return m.data }
func (m *testMessage) GetMetadata() map[string]interface{} {
	return map[string]interface{}{}
}

type recordingHandler struct {
	id    string
	calls *[]string
}

func (h *recordingHandler) Handle(_ context.Context, _ messaging.IMessage) error {
	*h.calls = append(*h.calls, h.id)
	return nil
}

func (h *recordingHandler) Type() string { return "recordingHandler" }

// TestMessageBus_MultipleHandlersSameType 确认同一 messageType 下多个同类型 handler 实例
// 会被视为不同订阅，并在 Publish 时都能被调用。
func TestMessageBus_MultipleHandlersSameType(t *testing.T) {
	transport := synctransport.NewSyncTransport()
	if err := transport.Start(context.Background()); err != nil {
		t.Fatalf("failed to start sync transport: %v", err)
	}
	bus := messaging.NewMessageBus(transport)

	var calls []string
	h1 := &recordingHandler{id: "h1", calls: &calls}
	h2 := &recordingHandler{id: "h2", calls: &calls}

	const msgType = "test-message"

	if err := bus.Subscribe(context.Background(), msgType, h1); err != nil {
		t.Fatalf("subscribe h1 failed: %v", err)
	}
	if err := bus.Subscribe(context.Background(), msgType, h2); err != nil {
		t.Fatalf("subscribe h2 failed: %v", err)
	}

	msg := &testMessage{id: "m1", typ: msgType, data: "payload"}
	if err := bus.Publish(context.Background(), msg); err != nil {
		t.Fatalf("publish failed: %v", err)
	}

	if len(calls) != 2 {
		t.Fatalf("expected 2 handler calls, got %d (%v)", len(calls), calls)
	}

	// 不强制要求顺序，但必须包含两个 handler 的调用记录
	seen := map[string]bool{}
	for _, id := range calls {
		seen[id] = true
	}
	if !seen["h1"] || !seen["h2"] {
		t.Fatalf("expected calls from h1 and h2, got %v", calls)
	}
}
