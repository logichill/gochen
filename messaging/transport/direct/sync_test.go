package direct

import (
	"context"
	"testing"
	"time"

	"gochen/errors"
	msg "gochen/messaging"
)

type incHandler struct{ n *int }

// Handle 处理消息并执行业务处理逻辑。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - m：消息数据
//
// 返回：
// - err：错误信息（nil 表示成功）
func (h incHandler) Handle(ctx context.Context, m msg.IMessage) error { *h.n++; return nil }

// Type 返回类型标识。
//
// 返回：
// - result：文本结果
func (h incHandler) Type() string { return "inc" }

type namedIncHandler struct {
	name string
	n    *int
}

func (h namedIncHandler) Handle(context.Context, msg.IMessage) error { *h.n++; return nil }
func (h namedIncHandler) Type() string                               { return h.name }

type panickingHandler struct{}

func (panickingHandler) Handle(context.Context, msg.IMessage) error { panic("boom") }
func (panickingHandler) Type() string                               { return "panicking" }

// TestSyncTransport_PublishFlow 验证 SyncTransport PublishFlow。
func TestSyncTransport_PublishFlow(t *testing.T) {
	tpt := NewSyncTransport()
	if err := tpt.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = tpt.Stop(context.Background()) }()

	var c int
	if _, err := tpt.Subscribe(context.Background(), "T", incHandler{n: &c}); err != nil {
		t.Fatalf("sub: %v", err)
	}

	msg := &msg.Message{ID: "1", Type: "T"}
	if err := tpt.Publish(context.Background(), msg); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if c != 1 {
		t.Fatalf("expected 1, got %d", c)
	}
}

func TestSyncTransport_WildcardSubscription(t *testing.T) {
	tpt := NewSyncTransport()
	if err := tpt.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = tpt.Stop(context.Background()) }()

	var exact, wildcard int
	if _, err := tpt.Subscribe(context.Background(), "T", namedIncHandler{name: "exact", n: &exact}); err != nil {
		t.Fatalf("sub exact: %v", err)
	}
	if _, err := tpt.Subscribe(context.Background(), "*", namedIncHandler{name: "wildcard", n: &wildcard}); err != nil {
		t.Fatalf("sub wildcard: %v", err)
	}

	if err := tpt.Publish(context.Background(), &msg.Message{ID: "1", Type: "T"}); err != nil {
		t.Fatalf("publish T: %v", err)
	}
	if exact != 1 || wildcard != 1 {
		t.Fatalf("expected exact=1 wildcard=1, got exact=%d wildcard=%d", exact, wildcard)
	}

	if err := tpt.Publish(context.Background(), &msg.Message{ID: "2", Type: "X"}); err != nil {
		t.Fatalf("publish X: %v", err)
	}
	if exact != 1 || wildcard != 2 {
		t.Fatalf("expected exact=1 wildcard=2, got exact=%d wildcard=%d", exact, wildcard)
	}
}

// TestSyncTransport_NotRunning 验证 SyncTransport NotRunning。
func TestSyncTransport_NotRunning(t *testing.T) {
	tpt := NewSyncTransport()
	if err := tpt.Publish(context.Background(), &msg.Message{ID: "x", Type: "T"}); err == nil {
		t.Fatalf("expected error when not running")
	}
}

func TestSyncTransport_StopCanceledContextStillStops(t *testing.T) {
	tpt := NewSyncTransport()
	if err := tpt.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := tpt.Stop(ctx); err != nil {
		t.Fatalf("stop with canceled context: %v", err)
	}
	if tpt.Stats().Running {
		t.Fatalf("expected transport to be stopped")
	}
}

func TestSyncTransport_PublishValidation(t *testing.T) {
	tpt := NewSyncTransport()
	_ = tpt.Start(context.Background())

	if err := tpt.Publish(nil, &msg.Message{ID: "1", Type: "T"}); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected InvalidInput for nil ctx, got: %v", err)
	}
	if err := tpt.Publish(context.Background(), nil); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected InvalidInput for nil message, got: %v", err)
	}
	if err := tpt.Publish(context.Background(), &msg.Message{ID: "1", Type: ""}); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected InvalidInput for empty type, got: %v", err)
	}

	if _, err := tpt.Subscribe(nil, "T", incHandler{}); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected InvalidInput for nil ctx, got: %v", err)
	}
	if _, err := tpt.Subscribe(context.Background(), "", incHandler{}); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected InvalidInput for empty messageType, got: %v", err)
	}
	if _, err := tpt.Subscribe(context.Background(), "T", nil); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected InvalidInput for nil handler, got: %v", err)
	}
}

func TestSyncTransport_HandlerPanicDoesNotCrash(t *testing.T) {
	tpt := NewSyncTransport()
	if err := tpt.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = tpt.Stop(context.Background()) }()

	var okCount int
	_, err := tpt.Subscribe(context.Background(), "T", panickingHandler{})
	if err != nil {
		t.Fatalf("sub panic: %v", err)
	}
	_, err = tpt.Subscribe(context.Background(), "T", namedIncHandler{name: "ok", n: &okCount})
	if err != nil {
		t.Fatalf("sub ok: %v", err)
	}

	err = tpt.Publish(context.Background(), &msg.Message{ID: "1", Type: "T"})
	if err == nil {
		t.Fatalf("expected publish error")
	}
	if !errors.Is(err, errors.Internal) {
		t.Fatalf("expected Internal error, got: %v", err)
	}
	if okCount != 1 {
		t.Fatalf("expected ok handler called once, got %d", okCount)
	}
}

func TestSyncTransport_PublishAll_NilMessageRejected(t *testing.T) {
	tpt := NewSyncTransport()
	if err := tpt.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = tpt.Stop(context.Background()) }()

	err := tpt.PublishAll(context.Background(), []msg.IMessage{nil})
	if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected InvalidInput, got: %v", err)
	}
}

type cancelingHandler struct{ cancel func() }

func (h cancelingHandler) Handle(_ context.Context, _ msg.IMessage) error { h.cancel(); return nil }
func (h cancelingHandler) Type() string                                   { return "canceling" }

type ctxErrHandler struct{}

func (ctxErrHandler) Handle(ctx context.Context, _ msg.IMessage) error {
	<-ctx.Done()
	return ctx.Err()
}
func (ctxErrHandler) Type() string { return "ctxErr" }

func TestSyncTransport_PublishAll_ContextCanceledNotWrapped(t *testing.T) {
	tpt := NewSyncTransport()
	if err := tpt.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = tpt.Stop(context.Background()) }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if _, err := tpt.Subscribe(context.Background(), "T", cancelingHandler{cancel: cancel}); err != nil {
		t.Fatalf("sub: %v", err)
	}

	err := tpt.PublishAll(ctx, []msg.IMessage{
		&msg.Message{ID: "1", Type: "T"},
		&msg.Message{ID: "2", Type: "T"},
	})
	if err != context.Canceled {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
}

func TestSyncTransport_PublishAll_DeadlineExceededNotWrapped(t *testing.T) {
	tpt := NewSyncTransport()
	if err := tpt.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = tpt.Stop(context.Background()) }()

	if _, err := tpt.Subscribe(context.Background(), "T", ctxErrHandler{}); err != nil {
		t.Fatalf("sub: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := tpt.PublishAll(ctx, []msg.IMessage{&msg.Message{ID: "1", Type: "T"}})
	if err != context.DeadlineExceeded {
		t.Fatalf("expected context.DeadlineExceeded, got: %v", err)
	}

}
