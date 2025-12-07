package projection

import (
	"context"
	"sync"
	"testing"
	"time"

	"gochen/eventing"
	"gochen/eventing/store"
	"gochen/messaging"
)

// TestProjectionEventHandler_ConcurrentHandleEvent
//
// 多个 goroutine 并发通过 projectionEventHandler 调用同一投影的 HandleEvent，
// 验证 ProjectionManager 的状态更新与 handler 内部锁在 -race 下无竞态。
func TestProjectionEventHandler_ConcurrentHandleEvent(t *testing.T) {
	eventStore := store.NewMemoryEventStore()
	eventBus := &MockEventBus{}
	manager := NewProjectionManager(eventStore, eventBus)

	projection := NewMockProjection("concurrent-projection", []string{"ConcurrentEvent"})

	if err := manager.RegisterProjection(projection); err != nil {
		t.Fatalf("register projection failed: %v", err)
	}

	if err := manager.StartProjection("concurrent-projection"); err != nil {
		t.Fatalf("start projection failed: %v", err)
	}

	// 构造事件
	evt := &eventing.Event[int64]{
		Message: messaging.Message{
			ID:        "evt-1",
			Type:      "ConcurrentEvent",
			Timestamp: time.Now(),
			Metadata:  make(map[string]any),
		},
	}

	// 从 manager 中取出对应的 handler
	manager.mutex.RLock()
	handler := manager.handlers["concurrent-projection"]["ConcurrentEvent"]
	manager.mutex.RUnlock()

	if handler == nil {
		t.Fatalf("expected handler for projection/event type not found")
	}

	const (
		goroutines = 16
		perGor     = 50
	)

	ctx := context.Background()
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < perGor; i++ {
				_ = handler.HandleEvent(ctx, evt)
			}
		}()
	}

	wg.Wait()

	status, err := manager.GetProjectionStatus("concurrent-projection")
	if err != nil {
		t.Fatalf("GetProjectionStatus failed: %v", err)
	}

	if status.ProcessedEvents <= 0 {
		t.Fatalf("expected some processed events, got %d", status.ProcessedEvents)
	}
}
