package bus

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"gochen/eventing"
	msg "gochen/messaging"
	synctransport "gochen/messaging/transport/sync"
)

type evHandler struct{ cnt *int32 }

func (h evHandler) Handle(ctx context.Context, evt interface{ GetType() string }) error { return nil }

type testEventHandler struct{ cnt *int32 }

func (h testEventHandler) Handle(ctx context.Context, m msg.IMessage) error {
	atomic.AddInt32(h.cnt, 1)
	return nil
}

func (h testEventHandler) HandleEvent(ctx context.Context, evt eventing.IEvent) error {
	atomic.AddInt32(h.cnt, 1)
	return nil
}

func (h testEventHandler) GetEventTypes() []string { return []string{"TestEvt"} }
func (h testEventHandler) GetHandlerName() string  { return "test" }
func (h testEventHandler) Type() string            { return "*" }

func TestEventBus_PublishSubscribe(t *testing.T) {
	// 使用同步传输，确保立即处理
	tpt := synctransport.NewSyncTransport()
	if err := tpt.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer tpt.Close()

	bus := msg.NewMessageBus(tpt)
	eb := NewEventBus(bus)

	var cnt int32
	h := testEventHandler{cnt: &cnt}
	if err := eb.SubscribeHandler(context.Background(), h); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	evt := &eventing.Event{
		Message: msg.Message{
			ID:        "evt-1",
			Type:      "TestEvt",
			Timestamp: time.Now(),
			Metadata:  make(map[string]any),
		},
		AggregateID:   1,
		AggregateType: "Agg",
		Version:       1,
	}
	if err := eb.PublishEvent(context.Background(), evt); err != nil {
		t.Fatalf("publish event: %v", err)
	}

	if atomic.LoadInt32(&cnt) == 0 {
		t.Fatalf("handler not invoked")
	}
}
