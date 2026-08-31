package memory

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gochen/contextx"
	"gochen/errors"
	"gochen/eventing"
	"gochen/messaging"
	dlqmem "gochen/messaging/deadletter/memory"
	"gochen/testkit/require"
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
func (h testHandler) Handle(ctx context.Context, m messaging.IMessage) error {
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
func (h blockingHandler) Handle(ctx context.Context, m messaging.IMessage) error {
	<-h.ch
	return nil
}

// Type 返回类型标识。
//
// 返回：
// - result：文本结果
func (h blockingHandler) Type() string { return "blockingHandler" }

type signaledBlockingHandler struct {
	started chan struct{}
	release chan struct{}
}

func (h signaledBlockingHandler) Handle(ctx context.Context, m messaging.IMessage) error {
	close(h.started)
	<-h.release
	return nil
}

func (h signaledBlockingHandler) Type() string { return "signaledBlockingHandler" }

type multiStartBlockingHandler struct {
	started chan string
	release chan struct{}
}

func (h multiStartBlockingHandler) Handle(ctx context.Context, m messaging.IMessage) error {
	h.started <- m.GetID()
	<-h.release
	return nil
}

func (h multiStartBlockingHandler) Type() string { return "multiStartBlockingHandler" }

type panickingTypeMessage struct{ *messaging.Message }

func (m *panickingTypeMessage) GetType() string { panic("type panic") }

type cloneCountingMessage struct {
	*messaging.Message
	clones *int32
}

func (m *cloneCountingMessage) CloneMessageEnvelope() messaging.IMessage {
	atomic.AddInt32(m.clones, 1)
	clone := *m.Message
	return &cloneCountingMessage{Message: &clone, clones: m.clones}
}

type blockingCloneMessage struct {
	*messaging.Message
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (m *blockingCloneMessage) CloneMessageEnvelope() messaging.IMessage {
	m.once.Do(func() {
		close(m.started)
	})
	<-m.release
	clone := *m.Message
	return &clone
}

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

	msg := &messaging.Message{ID: "m1", Type: "test"}
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

	if err := tpt.Publish(ctx, &messaging.Message{ID: "m1", Type: "test"}); err != nil {
		t.Fatalf("publish failed: %v", err)
	}
	if err := tpt.Publish(ctx, &messaging.Message{ID: "m2", Type: "test"}); err != nil {
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

		started := make(chan struct{})
		blockCh := make(chan struct{})
		t.Cleanup(func() { close(blockCh) })

		_, err := tpt.Subscribe(ctx, "block", signaledBlockingHandler{started: started, release: blockCh})
		require.NoError(t, err)
		require.NoError(t, tpt.Publish(ctx, &messaging.Message{ID: "m1", Type: "block"}))
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("handler did not start")
		}

		timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
		defer cancel()

		pending, err := tpt.StopWithSnapshot(timeoutCtx)
		require.Error(t, err)
		require.Len(t, pending, 1)
		require.Equal(t, "m1", pending[0].GetID())
	}
}

func TestMemoryTransport_StopWithSnapshotTimeoutIncludesProcessingAndQueued(t *testing.T) {
	ctx := context.Background()
	tpt := NewMemoryTransport(8, 1)
	require.NoError(t, tpt.Start(ctx))

	started := make(chan struct{})
	blockCh := make(chan struct{})
	t.Cleanup(func() { close(blockCh) })

	_, err := tpt.Subscribe(ctx, "block", signaledBlockingHandler{started: started, release: blockCh})
	require.NoError(t, err)
	require.NoError(t, tpt.Publish(ctx, &messaging.Message{ID: "m1", Type: "block"}))
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("handler did not start")
	}
	require.NoError(t, tpt.Publish(ctx, &messaging.Message{ID: "m2", Type: "block"}))
	require.NoError(t, tpt.Publish(ctx, &messaging.Message{ID: "m3", Type: "block"}))

	timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()
	pending, err := tpt.StopWithSnapshot(timeoutCtx)
	require.Error(t, err)

	ids := make(map[string]bool, len(pending))
	for _, message := range pending {
		ids[message.GetID()] = true
	}
	require.Equal(t, map[string]bool{"m1": true, "m2": true, "m3": true}, ids)
}

func TestMemoryTransport_StopWithSnapshotTimeoutIncludesAllWorkersAndQueue(t *testing.T) {
	ctx := context.Background()
	tpt := NewMemoryTransport(8, 2)
	require.NoError(t, tpt.Start(ctx))

	started := make(chan string, 2)
	blockCh := make(chan struct{})
	t.Cleanup(func() { close(blockCh) })

	_, err := tpt.Subscribe(ctx, "block", multiStartBlockingHandler{started: started, release: blockCh})
	require.NoError(t, err)
	require.NoError(t, tpt.Publish(ctx, &messaging.Message{ID: "m1", Type: "block"}))
	require.NoError(t, tpt.Publish(ctx, &messaging.Message{ID: "m2", Type: "block"}))
	require.NoError(t, tpt.Publish(ctx, &messaging.Message{ID: "m3", Type: "block"}))

	startedIDs := make(map[string]bool, 2)
	deadline := time.After(time.Second)
	for len(startedIDs) < 2 {
		select {
		case id := <-started:
			startedIDs[id] = true
		case <-deadline:
			t.Fatal("workers did not start processing two messages")
		}
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()
	pending, err := tpt.StopWithSnapshot(timeoutCtx)
	require.Error(t, err)

	ids := make(map[string]bool, len(pending))
	for _, message := range pending {
		ids[message.GetID()] = true
	}
	require.Equal(t, map[string]bool{"m1": true, "m2": true, "m3": true}, ids)
}

func TestMemoryTransport_SetProcessingAfterForceStopReturnsPending(t *testing.T) {
	tpt := NewMemoryTransport(8, 1)
	tpt.forceStop = true

	shouldProcess := tpt.setProcessing(0, &messaging.Message{ID: "m1", Type: "test"})

	require.False(t, shouldProcess)
	require.Empty(t, tpt.processing)
	require.Len(t, tpt.stoppedPending, 1)
	require.Equal(t, "m1", tpt.stoppedPending[0].GetID())
}

func TestMemoryTransport_WorkerStopsClaimingAfterForceStop(t *testing.T) {
	tpt := NewMemoryTransport(8, 1)
	queue := make(chan messaging.IMessage, 1)
	queue <- &messaging.Message{ID: "m1", Type: "test"}
	close(queue)
	stopCh := make(chan struct{})
	claimGate := &sync.Mutex{}

	tpt.forceStop = true
	tpt.wg.Add(1)
	go tpt.worker(context.Background(), queue, 0, stopCh, claimGate)
	tpt.wg.Wait()

	select {
	case got, ok := <-queue:
		require.True(t, ok)
		require.Equal(t, "m1", got.GetID())
	default:
		t.Fatal("worker consumed a queued message after forceStop")
	}
}

// TestMemoryTransport_StopWithPendingMessages 验证 MemoryTransport StopWithPendingMessages。
func TestMemoryTransport_StopWithPendingMessages(t *testing.T) {
	// 不启动 worker，避免消费队列中的消息，只验证 StopWithSnapshot drain 语义
	tpt := NewMemoryTransportForTest(4)
	ctx := context.Background()
	require.NoError(t, tpt.Start(ctx))

	// 塞入两条消息但不提供 handler，确保它们留在队列
	require.NoError(t, tpt.Publish(ctx, &messaging.Message{ID: "m1", Type: "none"}))
	require.NoError(t, tpt.Publish(ctx, &messaging.Message{ID: "m2", Type: "none"}))

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

	require.NoError(t, tpt.Publish(ctx, &messaging.Message{ID: "m1", Type: "test"}))
	err := tpt.Publish(ctx, &messaging.Message{ID: "m2", Type: "test"})
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

	err := tpt.Publish(ctx, &messaging.Message{ID: "m1", Type: "test"})
	require.Error(t, err)
}

func TestMemoryTransport_Publish_NilGuards(t *testing.T) {
	tpt := NewMemoryTransportForTest(4)
	ctx := context.Background()
	require.NoError(t, tpt.Start(ctx))
	t.Cleanup(func() { _ = tpt.Stop(context.Background()) })

	err := tpt.Publish(nil, &messaging.Message{ID: "m1", Type: "test"})
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))

	err = tpt.Publish(ctx, nil)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))

	err = tpt.Publish(ctx, &messaging.Message{ID: "m1", Type: ""})
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
func (h failingHandler) Handle(ctx context.Context, m messaging.IMessage) error {
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

func (h traceCaptureHandler) Handle(ctx context.Context, m messaging.IMessage) error {
	h.ch <- contextx.TraceID(ctx)
	return nil
}

func (h traceCaptureHandler) Type() string { return "traceCaptureHandler" }

type payloadCaptureHandler struct {
	ch chan map[string]any
}

func (h payloadCaptureHandler) Handle(ctx context.Context, m messaging.IMessage) error {
	payload, _ := messaging.PayloadAs[map[string]any](m.GetPayload())
	h.ch <- payload
	return nil
}

func (h payloadCaptureHandler) Type() string { return "payloadCaptureHandler" }

type mutatingHandler struct{}

func (h mutatingHandler) Handle(ctx context.Context, m messaging.IMessage) error {
	payload, _ := messaging.PayloadAs[map[string]any](m.GetPayload())
	payload["value"] = "mutated-by-first-handler"
	m.GetMetadata().Set("handler", "first")
	return nil
}

func (h mutatingHandler) Type() string { return "mutatingHandler" }

type mutatingFailingHandler struct{}

func (h mutatingFailingHandler) Handle(ctx context.Context, m messaging.IMessage) error {
	payload, _ := messaging.PayloadAs[map[string]any](m.GetPayload())
	payload["value"] = "mutated-before-error"
	m.GetMetadata().Set("handler", "mutating-failing")
	return fmt.Errorf("handler failed after mutation")
}

func (h mutatingFailingHandler) Type() string { return "mutatingFailingHandler" }

type metadataPayloadCaptureHandler struct {
	payloadCh  chan map[string]any
	metadataCh chan string
}

func (h metadataPayloadCaptureHandler) Handle(ctx context.Context, m messaging.IMessage) error {
	payload, _ := messaging.PayloadAs[map[string]any](m.GetPayload())
	h.payloadCh <- payload
	value, _ := m.GetMetadata().GetString("handler")
	h.metadataCh <- value
	return nil
}

func (h metadataPayloadCaptureHandler) Type() string { return "metadataPayloadCaptureHandler" }

type eventCaptureHandler struct {
	ch chan eventing.ITypedEvent[int64]
}

func (h eventCaptureHandler) Handle(ctx context.Context, m messaging.IMessage) error {
	_ = ctx
	evt, _ := m.(eventing.ITypedEvent[int64])
	h.ch <- evt
	return nil
}

func (h eventCaptureHandler) Type() string { return "eventCaptureHandler" }

func TestMemoryTransport_DispatchDerivesTraceID(t *testing.T) {
	tpt := NewMemoryTransport(16, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, tpt.Start(ctx))

	ch := make(chan string, 2)
	_, err := tpt.Subscribe(ctx, "trace", traceCaptureHandler{ch: ch})
	require.NoError(t, err)

	// 1) metadata 提供 trace_id：应回填到 ctx
	m1 := &messaging.Message{ID: "m1", Type: "trace"}
	m1.GetMetadata().Set(contextx.MetadataTraceKey, "trc-1")
	require.NoError(t, tpt.Publish(ctx, m1))

	select {
	case got := <-ch:
		require.Equal(t, "trc-1", got)
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("timeout waiting for handler")
	}

	// 2) metadata 缺失 trace_id：应使用 message.ID 作为 fallback
	m2 := &messaging.Message{ID: "m2", Type: "trace"}
	require.NoError(t, tpt.Publish(ctx, m2))

	select {
	case got := <-ch:
		require.Equal(t, "m2", got)
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("timeout waiting for handler")
	}

	require.NoError(t, tpt.Stop(context.Background()))
}

func TestMemoryTransport_PublishClonesMessageEnvelope(t *testing.T) {
	tpt := NewMemoryTransport(4, 1)
	ctx := context.Background()
	require.NoError(t, tpt.Start(ctx))
	t.Cleanup(func() { _ = tpt.Stop(context.Background()) })

	ch := make(chan map[string]any, 1)
	_, err := tpt.Subscribe(ctx, "snapshot", payloadCaptureHandler{ch: ch})
	require.NoError(t, err)

	payload := map[string]any{"value": "original"}
	message := &messaging.Message{ID: "m1", Type: "snapshot", Payload: messaging.NewPayload(payload)}
	require.NoError(t, tpt.Publish(ctx, message))

	payload["value"] = "mutated"
	message.Type = "mutated"
	message.ID = "mutated"

	select {
	case got := <-ch:
		require.Equal(t, "original", got["value"])
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("timeout waiting for handler")
	}
}

func TestMemoryTransport_PublishClonesEventEnvelopeForInterfaceHandlers(t *testing.T) {
	tpt := NewMemoryTransport(4, 1)
	ctx := context.Background()
	require.NoError(t, tpt.Start(ctx))
	t.Cleanup(func() { _ = tpt.Stop(context.Background()) })

	ch := make(chan eventing.ITypedEvent[int64], 1)
	_, err := tpt.Subscribe(ctx, "snapshot.event", eventCaptureHandler{ch: ch})
	require.NoError(t, err)

	payload := map[string]any{"value": "original"}
	event := eventing.NewEventWithID[int64]("snapshot-event-1", 42, "Task", "snapshot.event", 7, payload)
	event.GetMetadata().Set("trace_id", "trc-1")
	require.NoError(t, tpt.Publish(ctx, event))

	payload["value"] = "mutated"
	event.AggregateID = 99
	event.Type = "mutated"
	event.GetMetadata().Set("trace_id", "mutated")

	select {
	case got := <-ch:
		require.NotNil(t, got)
		require.Equal(t, int64(42), got.GetAggregateID())
		require.Equal(t, "Task", got.GetAggregateType())
		require.Equal(t, uint64(7), got.GetVersion())
		gotPayload := messaging.PayloadValue(got.GetPayload()).(map[string]any)
		require.Equal(t, "original", gotPayload["value"])
		traceID, ok := got.GetMetadata().GetString("trace_id")
		require.True(t, ok)
		require.Equal(t, "trc-1", traceID)
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("timeout waiting for handler")
	}
}

func TestMemoryTransport_PublishClonesNilMapSliceElements(t *testing.T) {
	tpt := NewMemoryTransport(4, 1)
	ctx := context.Background()
	require.NoError(t, tpt.Start(ctx))
	t.Cleanup(func() { _ = tpt.Stop(context.Background()) })

	ch := make(chan map[string]any, 1)
	_, err := tpt.Subscribe(ctx, "snapshot", payloadCaptureHandler{ch: ch})
	require.NoError(t, err)

	items := []map[string]any{nil, {"name": "original"}}
	message := &messaging.Message{ID: "m1", Type: "snapshot", Payload: messaging.NewPayload(map[string]any{"items": items})}
	require.NoError(t, tpt.Publish(ctx, message))

	items[1]["name"] = "mutated"

	select {
	case got := <-ch:
		gotItems := got["items"].([]map[string]any)
		require.Nil(t, gotItems[0])
		require.Equal(t, "original", gotItems[1]["name"])
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("timeout waiting for handler")
	}
}

func TestMemoryTransport_DispatchReusesSingleHandlerMessageClone(t *testing.T) {
	tpt := NewMemoryTransportForTest(4)
	ctx := context.Background()

	payloadCh := make(chan map[string]any, 1)
	metadataCh := make(chan string, 1)
	_, err := tpt.Subscribe(ctx, "snapshot", mutatingHandler{})
	require.NoError(t, err)
	_, err = tpt.Subscribe(ctx, "snapshot", metadataPayloadCaptureHandler{payloadCh: payloadCh, metadataCh: metadataCh})
	require.NoError(t, err)

	message := &messaging.Message{ID: "m1", Type: "snapshot", Payload: messaging.NewPayload(map[string]any{"value": "original"})}
	tpt.dispatch(ctx, message)

	select {
	case got := <-payloadCh:
		require.Equal(t, "mutated-by-first-handler", got["value"])
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("timeout waiting for payload")
	}
	select {
	case got := <-metadataCh:
		require.Equal(t, "first", got)
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("timeout waiting for metadata")
	}
}

func TestMemoryTransport_PublishAllFailsFastWhenQueueFull(t *testing.T) {
	tpt := NewMemoryTransportForTest(1)
	ctx := context.Background()
	require.NoError(t, tpt.Start(ctx))

	err := tpt.Publish(ctx, &messaging.Message{ID: "m1", Type: "test", Payload: messaging.NewPayload(map[string]any{"id": "m1"})})
	require.NoError(t, err)

	err = tpt.PublishAll(ctx, []messaging.IMessage{
		&messaging.Message{ID: "m2", Type: "test", Payload: messaging.NewPayload(map[string]any{"id": "m2"})},
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Queue))
	require.NoError(t, tpt.Stop(ctx))
}

func TestMemoryTransport_PublishAllDoesNotCloneWhenQueueFull(t *testing.T) {
	tpt := NewMemoryTransportForTest(1)
	ctx := context.Background()
	require.NoError(t, tpt.Start(ctx))

	require.NoError(t, tpt.Publish(ctx, &messaging.Message{ID: "m1", Type: "test"}))

	var clones int32
	err := tpt.PublishAll(ctx, []messaging.IMessage{
		&cloneCountingMessage{
			Message: &messaging.Message{ID: "m2", Type: "test"},
			clones:  &clones,
		},
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Queue))
	require.Equal(t, int32(0), atomic.LoadInt32(&clones))
	require.NoError(t, tpt.Stop(ctx))
}

func TestMemoryTransport_PublishDoesNotCloneWhenStopped(t *testing.T) {
	tpt := NewMemoryTransportForTest(1)
	ctx := context.Background()

	var clones int32
	err := tpt.Publish(ctx, &cloneCountingMessage{
		Message: &messaging.Message{ID: "m1", Type: "test"},
		clones:  &clones,
	})

	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Conflict))
	require.Equal(t, int32(0), atomic.LoadInt32(&clones))
}

func TestMemoryTransport_PublishAllDoesNotPartiallyEnqueueWhenBatchExceedsCapacity(t *testing.T) {
	tpt := NewMemoryTransportForTest(2)
	ctx := context.Background()
	require.NoError(t, tpt.Start(ctx))

	require.NoError(t, tpt.Publish(ctx, &messaging.Message{ID: "m1", Type: "test"}))

	err := tpt.PublishAll(ctx, []messaging.IMessage{
		&messaging.Message{ID: "m2", Type: "test"},
		&messaging.Message{ID: "m3", Type: "test"},
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Queue))

	pending, stopErr := tpt.StopWithSnapshot(ctx)
	require.NoError(t, stopErr)
	require.Len(t, pending, 1)
	require.Equal(t, "m1", pending[0].GetID())
}

func TestMemoryTransport_PublishAllDoesNotWaitForCapacity(t *testing.T) {
	tpt := NewMemoryTransportForTest(1)
	ctx := context.Background()
	require.NoError(t, tpt.Start(ctx))
	require.NoError(t, tpt.Publish(ctx, &messaging.Message{ID: "m1", Type: "test"}))

	started := time.Now()
	err := tpt.PublishAll(ctx, []messaging.IMessage{
		&messaging.Message{ID: "m2", Type: "test"},
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Queue))
	require.Less(t, time.Since(started), 50*time.Millisecond)
	require.NoError(t, tpt.Stop(ctx))
}

func TestMemoryTransport_PublishAllSerializesConcurrentCapacityCheckAndEnqueue(t *testing.T) {
	tpt := NewMemoryTransportForTest(2)
	ctx := context.Background()
	require.NoError(t, tpt.Start(ctx))

	cloneStarted := make(chan struct{})
	releaseClone := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- tpt.PublishAll(ctx, []messaging.IMessage{
			&blockingCloneMessage{
				Message: &messaging.Message{ID: "m1", Type: "test"},
				started: cloneStarted,
				release: releaseClone,
			},
			&messaging.Message{ID: "m2", Type: "test"},
		})
	}()

	select {
	case <-cloneStarted:
	case <-time.After(time.Second):
		t.Fatal("first PublishAll did not start cloning")
	}

	var secondClones int32
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- tpt.PublishAll(ctx, []messaging.IMessage{
			&cloneCountingMessage{
				Message: &messaging.Message{ID: "m3", Type: "test"},
				clones:  &secondClones,
			},
		})
	}()

	select {
	case err := <-secondDone:
		t.Fatalf("second PublishAll should wait for the first batch, got %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	require.Equal(t, int32(0), atomic.LoadInt32(&secondClones))

	close(releaseClone)
	require.NoError(t, <-firstDone)
	err := <-secondDone
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Queue))
	require.Equal(t, int32(0), atomic.LoadInt32(&secondClones))

	pending, stopErr := tpt.StopWithSnapshot(ctx)
	require.NoError(t, stopErr)
	require.Len(t, pending, 2)
	require.Equal(t, "m1", pending[0].GetID())
	require.Equal(t, "m2", pending[1].GetID())
}

func TestMemoryTransport_Dispatch_TraceID_ContextWins(t *testing.T) {
	tpt := NewMemoryTransportForTest(4)
	ctx := context.Background()

	ch := make(chan string, 1)
	_, err := tpt.Subscribe(ctx, "trace", traceCaptureHandler{ch: ch})
	require.NoError(t, err)

	base, err := contextx.WithTraceID(ctx, "ctx-trace")
	require.NoError(t, err)

	m := &messaging.Message{ID: "m1", Type: "trace"}
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

	require.NoError(t, tpt.Publish(ctx, &messaging.Message{ID: "m1", Type: "test"}))

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

func TestMemoryTransport_DeadLetterSink_UsesPreHandlerSnapshot(t *testing.T) {
	tpt := NewMemoryTransport(16, 1)
	ctx := context.Background()

	_, err := tpt.Subscribe(ctx, "test", mutatingFailingHandler{})
	require.NoError(t, err)

	sink := dlqmem.NewSink()
	tpt.SetDeadLetterSink(sink)

	message := &messaging.Message{ID: "m1", Type: "test", Payload: messaging.NewPayload(map[string]any{"value": "original"})}
	message.GetMetadata().Set("handler", "original")

	tpt.dispatch(ctx, message)

	entries := sink.Entries()
	require.Len(t, entries, 1)
	payload, ok := messaging.PayloadAs[map[string]any](entries[0].Message.GetPayload())
	require.True(t, ok)
	require.Equal(t, "original", payload["value"])
	metadata, ok := entries[0].Message.GetMetadata().GetString("handler")
	require.True(t, ok)
	require.Equal(t, "original", metadata)
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
func (h panickingHandler) Handle(ctx context.Context, m messaging.IMessage) error {
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

	require.NoError(t, tpt.Publish(ctx, &messaging.Message{ID: "m1", Type: "test"}))
	require.NoError(t, tpt.Publish(ctx, &messaging.Message{ID: "m2", Type: "test"}))

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

func TestMemoryTransport_PublishAllPropagatesMessagePanic(t *testing.T) {
	tpt := NewMemoryTransport(2, 1)
	defer func() { _ = tpt.Stop(context.Background()) }()
	require.NoError(t, tpt.Start(context.Background()))

	defer func() {
		if r := recover(); r != "type panic" {
			t.Fatalf("expected original panic to propagate, got %v", r)
		}
	}()
	_ = tpt.PublishAll(context.Background(), []messaging.IMessage{
		&panickingTypeMessage{Message: messaging.NewMessage("panic", messaging.KindEvent, "test", nil)},
	})
}

func TestMemoryTransport_PublishPropagatesMessagePanic(t *testing.T) {
	tpt := NewMemoryTransport(1, 1)
	defer func() { _ = tpt.Stop(context.Background()) }()
	require.NoError(t, tpt.Start(context.Background()))

	defer func() {
		if r := recover(); r != "type panic" {
			t.Fatalf("expected original panic to propagate, got %v", r)
		}
	}()
	_ = tpt.Publish(context.Background(), &panickingTypeMessage{Message: messaging.NewMessage("panic", messaging.KindEvent, "test", nil)})
}
