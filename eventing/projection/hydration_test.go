package projection

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"gochen/eventing"
	"gochen/eventing/bus"
	"gochen/eventing/registry"
	"gochen/eventing/store"
	"gochen/eventing/upcast"
	"gochen/messaging"
	synctransport "gochen/messaging/transport/direct"
)

type hydrationPayload struct{ Amount int }

type hydrationProjection struct {
	count int
}

// GetName 返回当前值。
//
// 返回：
// - result：文本结果
func (p *hydrationProjection) Name() string { return "hydration_projection" }

// Handle 处理消息并执行业务处理逻辑。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - event：事件数据
//
// 返回：
// - err：错误信息（nil 表示成功）
func (p *hydrationProjection) Handle(ctx context.Context, event eventing.IEvent) error {
	payload, ok := messaging.PayloadAs[*hydrationPayload](event.GetPayload())
	if !ok || payload == nil {
		return nil
	}
	p.count += payload.Amount
	return nil
}

func (p *hydrationProjection) HandleWithCheckpoint(ctx context.Context, event eventing.IEvent, store ICheckpointStore, checkpoint *Checkpoint) error {
	if err := p.Handle(ctx, event); err != nil {
		return err
	}
	if store != nil && checkpoint != nil {
		return store.Save(ctx, checkpoint)
	}
	return nil
}

// GetSupportedEventTypes 返回当前值。
//
// 返回：
// - result：列表结果（元素类型：string）
func (p *hydrationProjection) SupportedEventTypes() []string { return []string{"HydrationEvent"} }

// Rebuild ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
// - events：事件列表（待追加/发布）（类型：[]eventing.Event[int64]）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (p *hydrationProjection) Rebuild(ctx context.Context, events []eventing.Event[int64]) error {
	for i := range events {
		if err := p.Handle(ctx, &events[i]); err != nil {
			return err
		}
	}
	return nil
}

func (p *hydrationProjection) RebuildWithCheckpoint(ctx context.Context, events []eventing.Event[int64], store ICheckpointStore, checkpoint *Checkpoint) error {
	if err := p.Rebuild(ctx, events); err != nil {
		return err
	}
	if store != nil && checkpoint != nil {
		return store.Save(ctx, checkpoint)
	}
	return nil
}

// GetStatus 返回当前值。
//
// 返回：
// - result：测试返回值（类型：ProjectionStatus）
func (p *hydrationProjection) Status() ProjectionStatus {
	return ProjectionStatus{Name: p.Name()}
}

// TestProjectionManager_Replay_HydratesPayloadBestEffort 验证 ProjectionManager Replay HydratesPayloadBestEffort。
func TestProjectionManager_Replay_HydratesPayloadBestEffort(t *testing.T) {
	t.Parallel()

	reg := registry.NewRegistry()
	require.NoError(t, reg.Register("HydrationEvent", func() any { return &hydrationPayload{} }))
	upgraders := upcast.NewUpgraderRegistry()

	ctx := context.Background()
	eventStore := store.NewMemoryEventStore()

	// 以 map payload 模拟 SQL store 的反序列化形态
	storable := []eventing.IStorableEvent[int64]{
		eventing.NewEvent[int64](1, "HydrationAgg", "HydrationEvent", 1, map[string]any{"Amount": 2}),
		eventing.NewEvent[int64](1, "HydrationAgg", "HydrationEvent", 2, map[string]any{"Amount": 3}),
	}
	require.NoError(t, eventStore.AppendEvents(ctx, 1, storable, 0))

	transport := synctransport.NewSyncTransport()
	require.NoError(t, transport.Start(ctx))
	defer func() { _ = transport.Stop(context.Background()) }()

	messageBus := messaging.NewMessageBus(transport)
	eventBus := bus.NewEventBus(messageBus)

	pm, err := NewProjectionManager[int64](eventStore, eventBus, reg, upgraders)
	require.NoError(t, err)
	pm, err = pm.WithCheckpointStore(NewMemoryCheckpointStore())
	require.NoError(t, err)
	proj := &hydrationProjection{}
	require.NoError(t, pm.RegisterProjectionWithContext(ctx, proj))

	// 强制从头回放
	require.NoError(t, pm.ResumeFromCheckpoint(ctx, proj.Name()))

	require.Equal(t, 5, proj.count)
}
