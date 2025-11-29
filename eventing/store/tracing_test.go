package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gochen/eventing"
	httpx "gochen/http"
	"gochen/messaging/command"
)

// TestEndToEndTracing 端到端追踪测试
func TestEndToEndTracing(t *testing.T) {
	// 1. 模拟 HTTP 请求
	ctx := context.Background()
	correlationID := "cor-test-123"
	ctx = httpx.WithCorrelationID(ctx, correlationID)
	ctx = httpx.WithCausationID(ctx, "req-456")

	// 2. 创建命令
	cmd := command.NewCommand(
		"cmd-789",
		"CreateOrder",
		100,
		"Order",
		map[string]any{
			"product": "test",
		},
	)

	// 注入追踪 ID
	httpx.InjectTraceContext(ctx, cmd.Metadata)
	assert.Equal(t, correlationID, cmd.Metadata["correlation_id"])

	// 3. 命令执行后更新 causation_id
	ctx = httpx.WithCausationID(ctx, cmd.GetID())

	// 4. 创建事件
	event := eventing.NewEvent(100, "Order", "OrderCreated", 1, map[string]any{
		"order_id": 100,
	})

	// 5. 保存事件
	baseStore := NewMemoryEventStore()
	tracingStore := eventing.NewTracingEventStore(baseStore)

	err := tracingStore.AppendEvents(ctx, event.AggregateID, []eventing.IStorableEvent{event}, 0)
	require.NoError(t, err)

	// 6. 验证事件的追踪 ID
	assert.Equal(t, correlationID, event.Metadata["correlation_id"])
	assert.Equal(t, cmd.GetID(), event.Metadata["causation_id"])

	// 7. 加载并验证
	loadedEvents, err := tracingStore.LoadEvents(ctx, event.AggregateID, 0)
	require.NoError(t, err)
	require.Len(t, loadedEvents, 1)

	assert.Equal(t, correlationID, loadedEvents[0].Metadata["correlation_id"])
}

// TestTracingStore_MultipleEvents 测试多个事件
func TestTracingStore_MultipleEvents(t *testing.T) {
	ctx := context.Background()
	ctx = httpx.WithCorrelationID(ctx, "cor-batch-123")
	ctx = httpx.WithCausationID(ctx, "cmd-batch-456")

	// 创建多个事件
	events := []eventing.IStorableEvent{
		eventing.NewEvent(100, "Order", "OrderCreated", 1, map[string]any{"order_id": 100}),
		eventing.NewEvent(100, "Order", "OrderPaid", 2, map[string]any{"amount": 99.99}),
		eventing.NewEvent(100, "Order", "OrderShipped", 3, map[string]any{"tracking": "ABC123"}),
	}

	// 保存
	baseStore := NewMemoryEventStore()
	tracingStore := eventing.NewTracingEventStore(baseStore)

	err := tracingStore.AppendEvents(ctx, 100, events, 0)
	require.NoError(t, err)

	// 验证所有事件都有追踪 ID
	for i, event := range events {
		e := event.(*eventing.Event)
		assert.Equal(t, "cor-batch-123", e.Metadata["correlation_id"],
			"Event %d should have correlation_id", i)
	}
}

// TestTracingStore_WithoutContext 测试没有追踪上下文
func TestTracingStore_WithoutContext(t *testing.T) {
	ctx := context.Background()

	event := eventing.NewEvent(100, "Order", "OrderCreated", 1, nil)

	baseStore := NewMemoryEventStore()
	tracingStore := eventing.NewTracingEventStore(baseStore)

	err := tracingStore.AppendEvents(ctx, event.AggregateID, []eventing.IStorableEvent{event}, 0)
	require.NoError(t, err)

	// 没有追踪 ID
	assert.NotContains(t, event.Metadata, "correlation_id")
	assert.NotContains(t, event.Metadata, "causation_id")
}

// TestTraceContextChain 测试追踪链
func TestTraceContextChain(t *testing.T) {
	// 初始 HTTP 请求
	ctx := context.Background()
	correlationID := httpx.GenerateCorrelationID()
	ctx = httpx.WithCorrelationID(ctx, correlationID)
	ctx = httpx.WithCausationID(ctx, "http-req-123")

	// Command1
	cmd1 := command.NewCommand("cmd-1", "CreateOrder", 100, "Order", nil)
	httpx.InjectTraceContext(ctx, cmd1.Metadata)

	assert.Equal(t, correlationID, cmd1.Metadata["correlation_id"])
	assert.Equal(t, "http-req-123", cmd1.Metadata["causation_id"])

	// Command1 执行后更新 causation
	ctx = httpx.WithCausationID(ctx, cmd1.GetID())

	// Event1
	event1 := eventing.NewEvent(100, "Order", "OrderCreated", 1, nil)
	baseStore := NewMemoryEventStore()
	tracingStore := eventing.NewTracingEventStore(baseStore)
	err := tracingStore.AppendEvents(ctx, event1.AggregateID, []eventing.IStorableEvent{event1}, 0)
	require.NoError(t, err)

	assert.Equal(t, correlationID, event1.Metadata["correlation_id"])
	assert.Equal(t, cmd1.GetID(), event1.Metadata["causation_id"])

	// Event1 触发 Command2
	ctx = httpx.WithCausationID(ctx, event1.GetID())
	cmd2 := command.NewCommand("cmd-2", "SendEmail", 0, "Notification", nil)
	httpx.InjectTraceContext(ctx, cmd2.Metadata)

	assert.Equal(t, correlationID, cmd2.Metadata["correlation_id"])
	assert.Equal(t, event1.GetID(), cmd2.Metadata["causation_id"])

	// 验证整个链路的 correlation_id 一致
	assert.Equal(t, cmd1.Metadata["correlation_id"], event1.Metadata["correlation_id"])
	assert.Equal(t, event1.Metadata["correlation_id"], cmd2.Metadata["correlation_id"])
}
