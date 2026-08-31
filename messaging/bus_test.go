package messaging_test

import (
	"context"
	"testing"
	"time"

	"gochen/testkit/require"

	"gochen/errors"
	"gochen/messaging"
	"gochen/messaging/transport/direct"
	"gochen/messaging/transport/memory"
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

// CloneMessageEnvelope 返回测试消息副本，满足异步传输自定义消息克隆契约。
func (m *testMessage) CloneMessageEnvelope() messaging.IMessage {
	if m == nil {
		return nil
	}
	return &testMessage{
		id:   m.id,
		typ:  m.typ,
		data: m.data,
		meta: messaging.CloneMetadata(m.meta),
	}
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

func TestMessageBus_SubscribeRejectsTypedNilHandler(t *testing.T) {
	transport := direct.NewSyncTransport()
	require.NoError(t, transport.Start(context.Background()))
	t.Cleanup(func() { require.NoError(t, transport.Stop(context.Background())) })

	bus := messaging.NewMessageBus(transport)
	var handler *recordingHandler
	_, err := bus.Subscribe(context.Background(), "test", handler)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
}

// TestMessageBus_MultipleHandlersSameType 验证 MessageBus MultipleHandlersSameType。
func TestMessageBus_MultipleHandlersSameType(t *testing.T) {
	transport := direct.NewSyncTransport()
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

func TestMessageBus_UseNilMiddlewareIsNoop(t *testing.T) {
	transport := direct.NewSyncTransport()
	if err := transport.Start(context.Background()); err != nil {
		t.Fatalf("failed to start sync transport: %v", err)
	}
	bus := messaging.NewMessageBus(transport)
	bus.Use(nil)

	var calls []string
	unsub, err := bus.Subscribe(context.Background(), "noop-middleware", &recordingHandler{id: "h1", calls: &calls})
	if err != nil {
		t.Fatalf("subscribe failed: %v", err)
	}
	defer func() { _ = unsub(context.Background()) }()

	msg := &testMessage{id: "m1", typ: "noop-middleware", data: "payload"}
	if err := bus.Publish(context.Background(), msg); err != nil {
		t.Fatalf("publish failed: %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("expected handler call through nil middleware noop, got %v", calls)
	}
}

// TestMessageBus_HandlerPanic_ReturnsErrorAndCallsHook 验证 handler panic 不会被吞掉。
func TestMessageBus_HandlerPanic_ReturnsErrorAndCallsHook(t *testing.T) {
	transport := direct.NewSyncTransport()
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
	transport := direct.NewSyncTransport()
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

func TestMessageBus_PublishAll_MemoryTransportBatchCapacityNoPartialEnqueue(t *testing.T) {
	ctx := context.Background()
	transport := memory.NewMemoryTransportForTest(2)
	if err := transport.Start(ctx); err != nil {
		t.Fatalf("failed to start memory transport: %v", err)
	}

	if err := transport.Publish(ctx, &testMessage{id: "m1", typ: "test", data: "existing"}); err != nil {
		t.Fatalf("preload message failed: %v", err)
	}

	bus := messaging.NewMessageBus(transport)
	err := bus.PublishAll(ctx, []messaging.IMessage{
		&testMessage{id: "m2", typ: "test", data: "batch-1"},
		&testMessage{id: "m3", typ: "test", data: "batch-2"},
	})
	if err == nil {
		t.Fatalf("expected queue error, got nil")
	}
	if !errors.Is(err, errors.Queue) {
		t.Fatalf("expected queue error, got: %#v", err)
	}

	pending, stopErr := transport.StopWithSnapshot(ctx)
	if stopErr != nil {
		t.Fatalf("stop with snapshot failed: %v", stopErr)
	}
	if len(pending) != 1 {
		t.Fatalf("expected only pre-existing message to remain, got %d (%v)", len(pending), pending)
	}
	if got := pending[0].GetID(); got != "m1" {
		t.Fatalf("expected pre-existing message m1, got %q", got)
	}
}
