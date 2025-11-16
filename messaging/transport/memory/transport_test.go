package memory

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	msg "gochen/messaging"
)

type testHandler struct{ count *int32 }

func (h testHandler) Handle(ctx context.Context, m msg.IMessage) error {
	atomic.AddInt32(h.count, 1)
	return nil
}
func (h testHandler) Type() string { return "testHandler" }

func TestMemoryTransport_PublishFlow(t *testing.T) {
	tpt := NewMemoryTransport(16, 2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := tpt.Start(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	var cnt int32
	if err := tpt.Subscribe("test", testHandler{count: &cnt}); err != nil {
		t.Fatalf("subscribe failed: %v", err)
	}

	msg := &msg.Message{ID: "m1", Type: "test"}
	if err := tpt.Publish(ctx, msg); err != nil {
		t.Fatalf("publish failed: %v", err)
	}

	// 等待异步消费完成（最多 ~100ms）
	for i := 0; i < 20 && atomic.LoadInt32(&cnt) == 0; i++ {
		// 让出调度，等待 worker 处理
		// 使用短暂 sleep 避免忙等
		<-time.After(5 * time.Millisecond)
	}

	if atomic.LoadInt32(&cnt) == 0 {
		t.Fatalf("handler not invoked")
	}

	if err := tpt.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}
}
