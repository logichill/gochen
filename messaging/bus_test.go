package messaging_test

import (
	"context"
	"testing"
	"time"

	"gochen/errors"
	"gochen/messaging"
	synctransport "gochen/messaging/transport/direct"
)

type testMessage struct {
	id   string
	typ  string
	data string
	meta *messaging.Metadata
}

// GetID 返回当前值。
//
// 返回：
// - result：文本结果
func (m *testMessage) GetID() string { return m.id }

// GetKind 返回当前值。
//
// 返回：
// - result：测试返回值（类型：messaging.MessageKind）
func (m *testMessage) GetKind() messaging.MessageKind {
	return messaging.KindUnknown
}

// GetType 返回当前值。
//
// 返回：
// - result：文本结果
func (m *testMessage) GetType() string { return m.typ }

// GetTimestamp 返回当前值。
//
// 返回：
// - result：测试返回值（类型：time.Time）
func (m *testMessage) GetTimestamp() time.Time { return time.Now() }

// GetPayload 返回当前值。
//
// 返回：
// - result：测试载荷
func (m *testMessage) GetPayload() messaging.Payload { return messaging.NewPayload(m.data) }

// GetMetadata 返回当前值。
//
// 返回：
// - result：返回的实例（类型：*messaging.Metadata）
func (m *testMessage) GetMetadata() *messaging.Metadata {
	if m.meta == nil {
		m.meta = messaging.NewMetadata()
	}
	return m.meta
}

type recordingHandler struct {
	id    string
	calls *[]string
}

// Handle 处理消息并执行业务处理逻辑。
//
// 参数：
// - _：上下文（用于取消、超时与链路信息）
// - _：消息数据
//
// 返回：
// - err：错误信息（nil 表示成功）
func (h *recordingHandler) Handle(_ context.Context, _ messaging.IMessage) error {
	*h.calls = append(*h.calls, h.id)
	return nil
}

// Type 返回类型标识。
//
// 返回：
// - result：文本结果
func (h *recordingHandler) Type() string { return "recordingHandler" }

type panicHandler struct{}

// Handle 处理消息并执行业务处理逻辑。
//
// 参数：
// - _：上下文（用于取消、超时与链路信息）
// - _：消息数据
//
// 返回：
// - err：错误信息（nil 表示成功）
func (h *panicHandler) Handle(_ context.Context, _ messaging.IMessage) error {
	panic("boom")
}

// Type 返回类型标识。
//
// 返回：
// - result：文本结果
func (h *panicHandler) Type() string { return "panicHandler" }

// TestMessageBus_MultipleHandlersSameType 验证 MessageBus MultipleHandlersSameType。
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

	unsub1, err := bus.Subscribe(context.Background(), msgType, h1)
	if err != nil {
		t.Fatalf("subscribe h1 failed: %v", err)
	}
	defer func() { _ = unsub1(context.Background()) }()

	unsub2, err := bus.Subscribe(context.Background(), msgType, h2)
	if err != nil {
		t.Fatalf("subscribe h2 failed: %v", err)
	}
	defer func() { _ = unsub2(context.Background()) }()

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

// TestMessageBus_HandlerPanic_ReturnsErrorAndCallsHook 验证 handler panic 不会被吞掉。
func TestMessageBus_HandlerPanic_ReturnsErrorAndCallsHook(t *testing.T) {
	transport := synctransport.NewSyncTransport()
	if err := transport.Start(context.Background()); err != nil {
		t.Fatalf("failed to start sync transport: %v", err)
	}
	bus := messaging.NewMessageBus(transport)

	var hookCalls int
	var hookErr error
	bus.SetHandlerErrorHook(func(_ context.Context, _ messaging.IMessage, err error) {
		hookCalls++
		hookErr = err
	})

	const msgType = "panic-message"
	unsub, err := bus.Subscribe(context.Background(), msgType, &panicHandler{})
	if err != nil {
		t.Fatalf("subscribe failed: %v", err)
	}
	defer func() { _ = unsub(context.Background()) }()

	msg := &testMessage{id: "m1", typ: msgType, data: "payload"}
	err = bus.Publish(context.Background(), msg)
	if err == nil {
		t.Fatalf("expected publish error, got nil")
	}

	var appErr *errors.AppError
	if !errors.As(err, &appErr) || appErr == nil || appErr.Code() != errors.Internal {
		t.Fatalf("expected internal error, got: %#v", err)
	}

	if hookCalls != 1 {
		t.Fatalf("expected hookCalls=1, got %d", hookCalls)
	}
	if hookErr == nil {
		t.Fatalf("expected hookErr != nil")
	}
}

// TestMessageBus_PublishAll_NilMessage_FailFast 验证 PublishAll 对 nil message 做 fail-fast。
func TestMessageBus_PublishAll_NilMessage_FailFast(t *testing.T) {
	transport := synctransport.NewSyncTransport()
	if err := transport.Start(context.Background()); err != nil {
		t.Fatalf("failed to start sync transport: %v", err)
	}
	bus := messaging.NewMessageBus(transport)

	err := bus.PublishAll(context.Background(), []messaging.IMessage{nil})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}

	var appErr *errors.AppError
	if !errors.As(err, &appErr) || appErr == nil || appErr.Code() != errors.InvalidInput {
		t.Fatalf("expected invalid input error, got: %#v", err)
	}
}
