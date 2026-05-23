package store_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gochen/contextx"
	"gochen/eventing"
	estore "gochen/eventing/store"
	"gochen/eventing/store/decorators"
	"gochen/messaging/command"
)

// TestEndToEndTraceID 验证 EndToEndTraceID。
func TestEndToEndTraceID(t *testing.T) {
	// 1. 模拟请求上下文
	ctx := context.Background()
	traceID := "trc-test-123"
	ctx, err := contextx.WithTraceID(ctx, traceID)
	require.NoError(t, err)

	// 2. 创建命令
	cmd := command.NewCommand(
		"cmd-789",
		"CreateOrder",
		"100",
		"Order",
		map[string]any{
			"product": "test",
		},
	)

	// 注入 trace_id 到命令 metadata
	require.NoError(t, contextx.InjectTraceID(ctx, cmd.GetMetadata()))
	vTrace, ok := cmd.GetMetadata().GetString(contextx.MetadataTraceKey)
	assert.True(t, ok)
	assert.Equal(t, traceID, vTrace)

	// 3. 创建事件
	event := eventing.NewEvent[int64](100, "Order", "OrderCreated", 1, map[string]any{
		"order_id": 100,
	})

	// 4. 保存事件
	baseStore := estore.NewMemoryEventStore()
	tracingStore := decorators.NewTracingEventStore(baseStore)

	err = tracingStore.AppendEvents(ctx, event.AggregateID, []eventing.IStorableEvent[int64]{event}, 0)
	require.NoError(t, err)

	// 5. 验证事件的 trace_id
	vEvtTrace, ok := event.GetMetadata().GetString(contextx.MetadataTraceKey)
	assert.True(t, ok)
	assert.Equal(t, traceID, vEvtTrace)

	// 6. 加载并验证
	loadedEvents, err := tracingStore.LoadEvents(ctx, event.AggregateID, 0)
	require.NoError(t, err)
	require.Len(t, loadedEvents, 1)

	vLoaded, ok := loadedEvents[0].GetMetadata().GetString(contextx.MetadataTraceKey)
	assert.True(t, ok)
	assert.Equal(t, traceID, vLoaded)
}

// TestTracingStore_MultipleEvents 验证 TracingStore MultipleEvents。
func TestTracingStore_MultipleEvents(t *testing.T) {
	ctx := context.Background()
	ctx, err := contextx.WithTraceID(ctx, "trc-batch-123")
	require.NoError(t, err)

	// 创建多个事件
	events := []eventing.IStorableEvent[int64]{
		eventing.NewEvent[int64](100, "Order", "OrderCreated", 1, map[string]any{"order_id": 100}),
		eventing.NewEvent[int64](100, "Order", "OrderPaid", 2, map[string]any{"amount": 99.99}),
		eventing.NewEvent[int64](100, "Order", "OrderShipped", 3, map[string]any{"tracking": "ABC123"}),
	}

	// 保存
	baseStore := estore.NewMemoryEventStore()
	tracingStore := decorators.NewTracingEventStore(baseStore)

	err = tracingStore.AppendEvents(ctx, 100, events, 0)
	require.NoError(t, err)

	// 验证所有事件都有追踪 ID
	for i, event := range events {
		e := event.(*eventing.Event[int64])
		v, ok := e.GetMetadata().GetString(contextx.MetadataTraceKey)
		assert.True(t, ok, "Event %d should have trace_id", i)
		assert.Equal(t, "trc-batch-123", v, "Event %d should have trace_id", i)
	}
}

// TestTracingStore_WithoutContext 验证 TracingStore WithoutContext。
func TestTracingStore_WithoutContext(t *testing.T) {
	ctx := context.Background()

	event := eventing.NewEvent[int64](100, "Order", "OrderCreated", 1, nil)

	baseStore := estore.NewMemoryEventStore()
	tracingStore := decorators.NewTracingEventStore(baseStore)

	err := tracingStore.AppendEvents(ctx, event.AggregateID, []eventing.IStorableEvent[int64]{event}, 0)
	require.NoError(t, err)

	// 没有追踪 ID
	_, ok := event.GetMetadata().Get(contextx.MetadataTraceKey)
	assert.False(t, ok)
	_, ok = event.GetMetadata().Get("causation_id")
	assert.False(t, ok)
}

// TestTraceChain 验证 TraceChain。
func TestTraceChain(t *testing.T) {
	// 初始请求
	ctx := context.Background()
	traceID := "trc-123"
	ctx, err := contextx.WithTraceID(ctx, traceID)
	require.NoError(t, err)

	// Command1
	cmd1 := command.NewCommand("cmd-1", "CreateOrder", "100", "Order", nil)
	require.NoError(t, contextx.InjectTraceID(ctx, cmd1.GetMetadata()))

	vCmd1Trace, ok := cmd1.GetMetadata().GetString(contextx.MetadataTraceKey)
	assert.True(t, ok)
	assert.Equal(t, traceID, vCmd1Trace)

	// Event1
	event1 := eventing.NewEvent[int64](100, "Order", "OrderCreated", 1, nil)
	baseStore := estore.NewMemoryEventStore()
	tracingStore := decorators.NewTracingEventStore(baseStore)
	err = tracingStore.AppendEvents(ctx, event1.AggregateID, []eventing.IStorableEvent[int64]{event1}, 0)
	require.NoError(t, err)

	vEvent1Trace, ok := event1.GetMetadata().GetString(contextx.MetadataTraceKey)
	assert.True(t, ok)
	assert.Equal(t, traceID, vEvent1Trace)

	// Event1 触发 Command2（沿用同一 trace_id）
	cmd2 := command.NewCommand("cmd-2", "SendEmail", "", "Notification", nil)
	require.NoError(t, contextx.InjectTraceID(ctx, cmd2.GetMetadata()))

	vCmd2Trace, ok := cmd2.GetMetadata().GetString(contextx.MetadataTraceKey)
	assert.True(t, ok)
	assert.Equal(t, traceID, vCmd2Trace)

	// 验证整个链路的 trace_id 一致
	assert.Equal(t, vCmd1Trace, vEvent1Trace)
	assert.Equal(t, vEvent1Trace, vCmd2Trace)
}
