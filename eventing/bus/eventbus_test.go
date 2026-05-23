package bus

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"gochen/eventing"
	msg "gochen/messaging"
	synctransport "gochen/messaging/transport/direct"
)

type testEventHandler struct{ cnt *int32 }

// Handle 处理消息并执行业务处理逻辑。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - m：消息数据
//
// 返回：
// - err：错误信息（nil 表示成功）
func (h testEventHandler) Handle(ctx context.Context, m msg.IMessage) error {
	atomic.AddInt32(h.cnt, 1)
	return nil
}

// HandleEvent 处理事件并执行业务处理逻辑。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - evt：事件数据
//
// 返回：
// - err：错误信息（nil 表示成功）
func (h testEventHandler) HandleEvent(ctx context.Context, evt eventing.IEvent) error {
	atomic.AddInt32(h.cnt, 1)
	return nil
}

// EventTypes 返回处理器订阅的事件类型列表。
//
// 返回：
// - result：列表结果（元素类型：string）
func (h testEventHandler) EventTypes() []string { return []string{"TestEvt"} }

// HandlerName 返回处理器名称。
//
// 返回：
// - result：文本结果
func (h testEventHandler) HandlerName() string { return "test" }

// Type 返回类型标识。
//
// 返回：
// - result：文本结果
func (h testEventHandler) Type() string { return "*" }

// TestEventBus_PublishSubscribe 验证 EventBus PublishSubscribe。
func TestEventBus_PublishSubscribe(t *testing.T) {
	// 使用同步传输，确保立即处理
	tpt := synctransport.NewSyncTransport()
	if err := tpt.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = tpt.Stop(context.Background()) }()

	bus := msg.NewMessageBus(tpt)
	eb := NewEventBus(bus)

	var cnt int32
	h := testEventHandler{cnt: &cnt}
	unsub, err := eb.SubscribeHandler(context.Background(), h)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer func() { _ = unsub(context.Background()) }()

	evt := &eventing.Event[int64]{
		Message: msg.Message{
			ID:        "evt-1",
			Type:      "TestEvt",
			Timestamp: time.Now(),
			Metadata:  msg.NewMetadata(),
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
