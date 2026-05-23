package bus

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gochen/eventing"
	msg "gochen/messaging"
	synctransport "gochen/messaging/transport/direct"
)

// TestEventBus_ConcurrentPublishEvent_NoRace 验证 EventBus 在并发 PublishEvent 下无竞态。
func TestEventBus_ConcurrentPublishEvent_NoRace(t *testing.T) {
	tpt := synctransport.NewSyncTransport()
	if err := tpt.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = tpt.Stop(context.Background()) }()

	mb := msg.NewMessageBus(tpt)
	eb := NewEventBus(mb)

	var cnt int32
	h := testEventHandler{cnt: &cnt}
	unsub, err := eb.SubscribeHandler(context.Background(), h)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer func() { _ = unsub(context.Background()) }()

	const (
		goroutines = 16
		perGor     = 50
	)

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perGor; i++ {
				evt := &eventing.Event[int64]{
					Message: msg.Message{
						ID:        fmt.Sprintf("evt-%d-%d", g, i),
						Type:      "TestEvt",
						Timestamp: time.Now(),
						Metadata:  msg.NewMetadata(),
					},
					AggregateID:   int64(g + 1),
					AggregateType: "Agg",
					Version:       uint64(i + 1),
				}
				_ = eb.PublishEvent(context.Background(), evt)
			}
		}(g)
	}

	wg.Wait()
	if atomic.LoadInt32(&cnt) == 0 {
		t.Fatalf("handler not invoked")
	}
}
