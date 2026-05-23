package memory

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gochen/contextx"
	"gochen/errors"
	msg "gochen/messaging"
	dlqmem "gochen/messaging/deadletter/memory"
)

type testHandler struct{ count *int32 }

// Handle 处理消息并执行业务处理逻辑。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - m：消息数据
//
// 返回：
// - err：错误信息（nil 表示成功）
func (h testHandler) Handle(ctx context.Context, m msg.IMessage) error {
	atomic.AddInt32(h.count, 1)
	return nil
}

// Type 返回类型标识。
//
// 返回：
// - result：文本结果
func (h testHandler) Type() string { return "testHandler" }

// 阻塞处理器用于测试关闭超时
type blockingHandler struct{ ch chan struct{} }

// Handle 处理消息并执行业务处理逻辑。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - m：消息数据
//
// 返回：
// - err：错误信息（nil 表示成功）
func (h blockingHandler) Handle(ctx context.Context, m msg.IMessage) error {
	<-h.ch
	return nil
}

// Type 返回类型标识。
//
// 返回：
// - result：文本结果
func (h blockingHandler) Type() string { return "blockingHandler" }

// TestMemoryTransport_PublishFlow 验证 MemoryTransport PublishFlow。
func TestMemoryTransport_PublishFlow(t *testing.T) {
	tpt := NewMemoryTransport(16, 2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := tpt.Start(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	var cnt int32
	if _, err := tpt.Subscribe(ctx, "test", testHandler{count: &cnt}); err != nil {
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

	if err := tpt.Stop(context.Background()); err != nil {
		t.Fatalf("stop failed: %v", err)
	}
}

// TestMemoryTransport_StopDrainsQueue 验证 MemoryTransport StopDrainsQueue。
func TestMemoryTransport_StopDrainsQueue(t *testing.T) {
	tpt := NewMemoryTransport(16, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := tpt.Start(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	var cnt int32
	if _, err := tpt.Subscribe(ctx, "test", testHandler{count: &cnt}); err != nil {
		t.Fatalf("subscribe failed: %v", err)
	}

	if err := tpt.Publish(ctx, &msg.Message{ID: "m1", Type: "test"}); err != nil {
		t.Fatalf("publish failed: %v", err)
	}
	if err := tpt.Publish(ctx, &msg.Message{ID: "m2", Type: "test"}); err != nil {
		t.Fatalf("publish failed: %v", err)
	}

	if err := tpt.Stop(context.Background()); err != nil {
		t.Fatalf("stop failed: %v", err)
	}

	if atomic.LoadInt32(&cnt) != 2 {
		t.Fatalf("expected 2 messages processed before stop, got %d", cnt)
	}
}

// TestMemoryTransport_StopWithSnapshotTimeout 验证 MemoryTransport StopWithSnapshotTimeout。
func TestMemoryTransport_StopWithSnapshotTimeout(t *testing.T) {
	ctx := context.Background()

	// StopWithSnapshot 超时
	{
		tpt := NewMemoryTransport(4, 1)
		require.NoError(t, tpt.Start(ctx))

		blockCh := make(chan struct{})
		t.Cleanup(func() { close(blockCh) })

		_, err := tpt.Subscribe(ctx, "block", blockingHandler{ch: blockCh})
		require.NoError(t, err)
		require.NoError(t, tpt.Publish(ctx, &msg.Message{ID: "m1", Type: "block"}))

		timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
		defer cancel()

		_, err = tpt.StopWithSnapshot(timeoutCtx)
		require.Error(t, err)
	}
}

// TestMemoryTransport_StopWithPendingMessages 验证 MemoryTransport StopWithPendingMessages。
func TestMemoryTransport_StopWithPendingMessages(t *testing.T) {
	// 不启动 worker，避免消费队列中的消息，只验证 StopWithSnapshot drain 语义
	tpt := NewMemoryTransportForTest(4)
	ctx := context.Background()
	require.NoError(t, tpt.Start(ctx))

	// 塞入两条消息但不提供 handler，确保它们留在队列
	require.NoError(t, tpt.Publish(ctx, &msg.Message{ID: "m1", Type: "none"}))
	require.NoError(t, tpt.Publish(ctx, &msg.Message{ID: "m2", Type: "none"}))

	pending, err := tpt.StopWithSnapshot(ctx)
	require.NoError(t, err)
	require.Len(t, pending, 2)
}

// TestMemoryTransport_PublishFailsWhenQueueFull 验证 MemoryTransport PublishFailsWhenQueueFull。
func TestMemoryTransport_PublishFailsWhenQueueFull(t *testing.T) {
	// 不启动 worker，固定制造队列满的故障场景
	tpt := NewMemoryTransportForTest(1)
	ctx := context.Background()
	require.NoError(t, tpt.Start(ctx))

	require.NoError(t, tpt.Publish(ctx, &msg.Message{ID: "m1", Type: "test"}))
	err := tpt.Publish(ctx, &msg.Message{ID: "m2", Type: "test"})
	require.Error(t, err)

	_, cerr := tpt.StopWithSnapshot(ctx)
	require.NoError(t, cerr)
}

// TestMemoryTransport_PublishAfterStopFails 验证 MemoryTransport PublishAfterStopFails。
func TestMemoryTransport_PublishAfterStopFails(t *testing.T) {
	tpt := NewMemoryTransportForTest(4)
	ctx := context.Background()
	require.NoError(t, tpt.Start(ctx))
	require.NoError(t, tpt.Stop(context.Background()))

	err := tpt.Publish(ctx, &msg.Message{ID: "m1", Type: "test"})
	require.Error(t, err)
}

func TestMemoryTransport_Publish_NilGuards(t *testing.T) {
	tpt := NewMemoryTransportForTest(4)
	ctx := context.Background()
	require.NoError(t, tpt.Start(ctx))
	t.Cleanup(func() { _ = tpt.Stop(context.Background()) })

	err := tpt.Publish(nil, &msg.Message{ID: "m1", Type: "test"})
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))

	err = tpt.Publish(ctx, nil)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))

	err = tpt.Publish(ctx, &msg.Message{ID: "m1", Type: ""})
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
}

func TestMemoryTransport_Subscribe_NilGuards(t *testing.T) {
	tpt := NewMemoryTransportForTest(4)
	ctx := context.Background()

	_, err := tpt.Subscribe(nil, "t", testHandler{})
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))

	_, err = tpt.Subscribe(ctx, "", testHandler{})
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))

	_, err = tpt.Subscribe(ctx, "t", nil)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
}

type failingHandler struct{}

// Handle 处理消息并执行业务处理逻辑。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - m：消息数据
//
// 返回：
// - err：错误信息（nil 表示成功）
func (h failingHandler) Handle(ctx context.Context, m msg.IMessage) error {
	_ = ctx
	_ = m
	return fmt.Errorf("handler failed")
}

// Type 返回类型标识。
//
// 返回：
// - result：文本结果
func (h failingHandler) Type() string { return "failingHandler" }

type traceCaptureHandler struct{ ch chan string }

func (h traceCaptureHandler) Handle(ctx context.Context, m msg.IMessage) error {
	h.ch <- contextx.TraceID(ctx)
	return nil
}

func (h traceCaptureHandler) Type() string { return "traceCaptureHandler" }

func TestMemoryTransport_DispatchDerivesTraceID(t *testing.T) {
	tpt := NewMemoryTransport(16, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, tpt.Start(ctx))

	ch := make(chan string, 2)
	_, err := tpt.Subscribe(ctx, "trace", traceCaptureHandler{ch: ch})
	require.NoError(t, err)

	// 1) metadata 提供 trace_id：应回填到 ctx
	m1 := &msg.Message{ID: "m1", Type: "trace"}
	m1.GetMetadata().Set(contextx.MetadataTraceKey, "trc-1")
	require.NoError(t, tpt.Publish(ctx, m1))

	select {
	case got := <-ch:
		require.Equal(t, "trc-1", got)
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("timeout waiting for handler")
	}

	// 2) metadata 缺失 trace_id：应使用 message.ID 作为 fallback
	m2 := &msg.Message{ID: "m2", Type: "trace"}
	require.NoError(t, tpt.Publish(ctx, m2))

	select {
	case got := <-ch:
		require.Equal(t, "m2", got)
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("timeout waiting for handler")
	}

	require.NoError(t, tpt.Stop(context.Background()))
}

func TestMemoryTransport_Dispatch_TraceID_ContextWins(t *testing.T) {
	tpt := NewMemoryTransportForTest(4)
	ctx := context.Background()

	ch := make(chan string, 1)
	_, err := tpt.Subscribe(ctx, "trace", traceCaptureHandler{ch: ch})
	require.NoError(t, err)

	base, err := contextx.WithTraceID(ctx, "ctx-trace")
	require.NoError(t, err)

	m := &msg.Message{ID: "m1", Type: "trace"}
	m.GetMetadata().Set(contextx.MetadataTraceKey, "md-trace")

	tpt.dispatch(base, m)

	select {
	case got := <-ch:
		require.Equal(t, "ctx-trace", got)
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("timeout waiting for handler")
	}

	got, _ := m.GetMetadata().Get(contextx.MetadataTraceKey)
	require.Equal(t, "ctx-trace", got)
}

// TestMemoryTransport_DeadLetterSink_OnHandlerError 验证 MemoryTransport DeadLetterSink OnHandlerError。
func TestMemoryTransport_DeadLetterSink_OnHandlerError(t *testing.T) {
	tpt := NewMemoryTransport(16, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, tpt.Start(ctx))
	_, err := tpt.Subscribe(ctx, "test", failingHandler{})
	require.NoError(t, err)

	sink := dlqmem.NewSink()
	tpt.SetDeadLetterSink(sink)

	require.NoError(t, tpt.Publish(ctx, &msg.Message{ID: "m1", Type: "test"}))

	// 等待异步处理写入 DLQ（最多 ~200ms）
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if len(sink.Entries()) > 0 {
			break
		}
		<-time.After(5 * time.Millisecond)
	}

	entries := sink.Entries()
	require.Len(t, entries, 1)
	require.Equal(t, "failingHandler", entries[0].HandlerType)
	require.Equal(t, "m1", entries[0].Message.GetID())
	require.Equal(t, "test", entries[0].Message.GetType())
	require.Error(t, entries[0].Err)
}

type panickingHandler struct{ count *int32 }

// Handle 处理消息并执行业务处理逻辑。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - m：消息数据
//
// 返回：
// - err：错误信息（nil 表示成功）
func (h panickingHandler) Handle(ctx context.Context, m msg.IMessage) error {
	_ = ctx
	_ = m
	atomic.AddInt32(h.count, 1)
	panic("boom")
}

// Type 返回类型标识。
//
// 返回：
// - result：文本结果
func (h panickingHandler) Type() string { return "panickingHandler" }

// TestMemoryTransport_DeadLetterSink_OnHandlerPanic_DoesNotKillWorker 验证 MemoryTransport DeadLetterSink OnHandlerPanic DoesNotKillWorker。
func TestMemoryTransport_DeadLetterSink_OnHandlerPanic_DoesNotKillWorker(t *testing.T) {
	tpt := NewMemoryTransport(16, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, tpt.Start(ctx))

	var panics int32
	var okCount int32

	// 先注册会 panic 的 handler，再注册正常 handler：
	// - 若不 recover，会导致 worker goroutine 退出，ok handler 不会被调用；
	// - recover 后，ok handler 仍会被调用，且 worker 继续处理后续消息。
	_, err := tpt.Subscribe(ctx, "test", panickingHandler{count: &panics})
	require.NoError(t, err)
	_, err = tpt.Subscribe(ctx, "test", testHandler{count: &okCount})
	require.NoError(t, err)

	sink := dlqmem.NewSink()
	tpt.SetDeadLetterSink(sink)

	require.NoError(t, tpt.Publish(ctx, &msg.Message{ID: "m1", Type: "test"}))
	require.NoError(t, tpt.Publish(ctx, &msg.Message{ID: "m2", Type: "test"}))

	// 等待异步处理完成（最多 ~300ms）
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&okCount) >= 2 && len(sink.Entries()) >= 2 {
			break
		}
		<-time.After(5 * time.Millisecond)
	}

	require.Equal(t, int32(2), atomic.LoadInt32(&panics))
	require.Equal(t, int32(2), atomic.LoadInt32(&okCount))

	entries := sink.Entries()
	require.Len(t, entries, 2)
	require.Equal(t, "panickingHandler", entries[0].HandlerType)
	require.Equal(t, "panickingHandler", entries[1].HandlerType)
	require.Error(t, entries[0].Err)
	require.Error(t, entries[1].Err)

	require.NoError(t, tpt.Stop(context.Background()))
}
