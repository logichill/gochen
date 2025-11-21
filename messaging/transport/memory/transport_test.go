package memory

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	msg "gochen/messaging"
)

type testHandler struct{ count *int32 }

func (h testHandler) Handle(ctx context.Context, m msg.IMessage) error {
	atomic.AddInt32(h.count, 1)
	return nil
}
func (h testHandler) Type() string { return "testHandler" }

// 阻塞处理器用于测试关闭超时
type blockingHandler struct{ ch chan struct{} }

func (h blockingHandler) Handle(ctx context.Context, m msg.IMessage) error {
	<-h.ch
	return nil
}
func (h blockingHandler) Type() string { return "blockingHandler" }

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

func TestMemoryTransport_CloseDrainsQueue(t *testing.T) {
	tpt := NewMemoryTransport(16, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := tpt.Start(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	var cnt int32
	if err := tpt.Subscribe("test", testHandler{count: &cnt}); err != nil {
		t.Fatalf("subscribe failed: %v", err)
	}

	if err := tpt.Publish(ctx, &msg.Message{ID: "m1", Type: "test"}); err != nil {
		t.Fatalf("publish failed: %v", err)
	}
	if err := tpt.Publish(ctx, &msg.Message{ID: "m2", Type: "test"}); err != nil {
		t.Fatalf("publish failed: %v", err)
	}

	if err := tpt.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	if atomic.LoadInt32(&cnt) != 2 {
		t.Fatalf("expected 2 messages processed before close, got %d", cnt)
	}
}

func TestMemoryTransport_CloseWithContextTimeout(t *testing.T) {
	tpt := NewMemoryTransport(4, 1)
	ctx := context.Background()
	require.NoError(t, tpt.Start(ctx))

	blockCh := make(chan struct{})
	t.Cleanup(func() { close(blockCh) })

	require.NoError(t, tpt.Subscribe("block", blockingHandler{ch: blockCh}))

	require.NoError(t, tpt.Publish(ctx, &msg.Message{ID: "m1", Type: "block"}))

	timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()

	_, err := tpt.CloseWithContext(timeoutCtx)
	require.Error(t, err)

	// 确认 CloseWithTimeout 也能返回超时
	err = tpt.CloseWithTimeout(10 * time.Millisecond)
	require.Error(t, err)
}

func TestMemoryTransport_CloseWithPendingMessages(t *testing.T) {
	// 不启动 worker，避免消费队列中的消息，只验证 CloseWithContext drain 语义
	tpt := NewMemoryTransportForTest(4)
	ctx := context.Background()
	require.NoError(t, tpt.Start(ctx))

	// 塞入两条消息但不提供 handler，确保它们留在队列
	require.NoError(t, tpt.Publish(ctx, &msg.Message{ID: "m1", Type: "none"}))
	require.NoError(t, tpt.Publish(ctx, &msg.Message{ID: "m2", Type: "none"}))

	pending, err := tpt.CloseWithContext(ctx)
	require.NoError(t, err)
	require.Len(t, pending, 2)
}
