package messaging_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gochen/messaging"
	"gochen/messaging/transport/memory"
)

// TestMessageBus_WithMemoryTransport_ConcurrentPublish 验证 MessageBus WithMemoryTransport ConcurrentPublish。
func TestMessageBus_WithMemoryTransport_ConcurrentPublish(t *testing.T) {
	tpt := memory.NewMemoryTransport(1024, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := tpt.Start(ctx); err != nil {
		t.Fatalf("failed to start memory transport: %v", err)
	}

	bus := messaging.NewMessageBus(tpt)

	var handled int32
	handler := &recordingHandler{
		id:    "concurrent",
		calls: &[]string{}, // not used here, we只用原子计数
	}

	// 包装一个带计数的 handler
	wrapped := &countingHandler{
		IMessageHandler: handler,
		counter:         &handled,
	}

	const msgType = "concurrent-test"

	unsub, err := bus.Subscribe(ctx, msgType, wrapped)
	if err != nil {
		t.Fatalf("subscribe failed: %v", err)
	}
	defer func() { _ = unsub(context.Background()) }()

	const (
		goroutines = 8
		perGor     = 200
		total      = goroutines * perGor
	)

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < perGor; i++ {
				msg := &testMessage{
					id:   fmt.Sprintf("m-%d-%d", id, i),
					typ:  msgType,
					data: "payload",
				}
				_ = bus.Publish(ctx, msg)
			}
		}(g)
	}

	wg.Wait()

	// 等待 MemoryTransport 异步 worker 完成处理
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&handled) >= int32(total) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if got := atomic.LoadInt32(&handled); got == 0 {
		t.Fatalf("no messages were handled in concurrent publish test")
	}
}

// countingHandler 为现有 recordingHandler 增加原子计数，用于并发测试统计。
type countingHandler struct {
	messaging.IMessageHandler
	counter *int32
}

// Handle 处理消息并执行业务处理逻辑。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - msg：消息数据
//
// 返回：
// - err：错误信息（nil 表示成功）
func (h *countingHandler) Handle(ctx context.Context, msg messaging.IMessage) error {
	atomic.AddInt32(h.counter, 1)
	// 这里不再调用内部 handler，以避免在并发测试中对原有
	// recordingHandler.calls 切片产生额外的竞态访问。
	return nil
}
