package projection

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gochen/eventing"
	"gochen/eventing/bus"
	"gochen/eventing/registry"
	"gochen/eventing/store"
	"gochen/eventing/upcast"
	msg "gochen/messaging"
	synctransport "gochen/messaging/transport/direct"
)

func TestIntegration_CriticalChain_EventStore_Projection_Checkpoint(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	es := store.NewMemoryEventStore()
	cs := NewMemoryCheckpointStore()

	tpt := synctransport.NewSyncTransport()
	require.NoError(t, tpt.Start(ctx))
	defer func() { _ = tpt.Stop(context.Background()) }()

	mb := msg.NewMessageBus(tpt)
	eventBus := bus.NewEventBus(mb)
	reg := registry.NewRegistry()
	require.NoError(t, reg.Register("UserCreated", func() any { return &struct{}{} }))
	upgraders := upcast.NewUpgraderRegistry()

	p := NewMockProjection("proj", []string{"UserCreated"})
	cfg := ProjectionConfig{
		CheckpointSaveInterval: 0,
		CheckpointSaveCount:    1,
	}
	pm, err := NewProjectionManagerWithConfig[int64](es, eventBus, reg, upgraders, &cfg)
	require.NoError(t, err)
	pm, err = pm.WithCheckpointStore(cs)
	require.NoError(t, err)

	require.NoError(t, pm.RegisterProjectionWithContext(ctx, p))
	require.NoError(t, pm.StartProjection("proj"))

	appendOne := func(id string, ts time.Time) {
		ver, err := es.GetAggregateVersion(ctx, 1)
		require.NoError(t, err)
		expected := ver

		e := *eventing.NewEvent[int64](1, "user", "UserCreated", ver+1, nil)
		e.ID = id
		e.Timestamp = ts
		require.NoError(t, es.AppendEvents(ctx, 1, toStorableEvents([]eventing.Event[int64]{e}), expected))
	}

	appendOne("e1", time.Now().Add(-2*time.Second))
	appendOne("e2", time.Now().Add(-1*time.Second))
	appendOne("e3", time.Now())

	// 事件存储与投影处理是通过 EventBus 订阅来驱动的，因此这里需要显式发布事件。
	require.NoError(t, eventBus.PublishEvent(ctx, &eventing.Event[int64]{
		Message: msg.Message{
			ID:        "e1",
			Kind:      msg.KindEvent,
			Type:      "UserCreated",
			Timestamp: time.Now().Add(-2 * time.Second),
			Payload:   msg.NewPayload(nil),
			Metadata:  msg.NewMetadata(),
		},
		AggregateID:   1,
		AggregateType: "user",
		Version:       1,
		SchemaVersion: 1,
	}))
	require.NoError(t, eventBus.PublishEvent(ctx, &eventing.Event[int64]{
		Message: msg.Message{
			ID:        "e2",
			Kind:      msg.KindEvent,
			Type:      "UserCreated",
			Timestamp: time.Now().Add(-1 * time.Second),
			Payload:   msg.NewPayload(nil),
			Metadata:  msg.NewMetadata(),
		},
		AggregateID:   1,
		AggregateType: "user",
		Version:       2,
		SchemaVersion: 1,
	}))
	require.NoError(t, eventBus.PublishEvent(ctx, &eventing.Event[int64]{
		Message: msg.Message{
			ID:        "e3",
			Kind:      msg.KindEvent,
			Type:      "UserCreated",
			Timestamp: time.Now(),
			Payload:   msg.NewPayload(nil),
			Metadata:  msg.NewMetadata(),
		},
		AggregateID:   1,
		AggregateType: "user",
		Version:       3,
		SchemaVersion: 1,
	}))

	require.Eventually(t, func() bool {
		st := p.Status()
		return st.ProcessedEvents == 3
	}, 2*time.Second, 10*time.Millisecond)

	ck, err := cs.Load(ctx, "proj")
	require.NoError(t, err)
	require.NotNil(t, ck)
	require.Equal(t, int64(3), ck.Position)
	require.Equal(t, "e3", ck.LastEventID)

	// 重新装配一个投影管理器（使用独立的 checkpoint store），
	// 验证“同一条关键链路”仍能工作。
	require.NoError(t, pm.StopProjection("proj"))

	p2 := NewMockProjection("proj", []string{"UserCreated"})
	cs2 := NewMemoryCheckpointStore()
	pm2, err := NewProjectionManagerWithConfig[int64](es, eventBus, reg, upgraders, &cfg)
	require.NoError(t, err)
	pm2, err = pm2.WithCheckpointStore(cs2)
	require.NoError(t, err)
	require.NoError(t, pm2.RegisterProjectionWithContext(ctx, p2))
	require.NoError(t, pm2.StartProjection("proj"))
	defer func() { _ = pm2.StopProjection("proj") }()

	appendOne("e4", time.Now().Add(1*time.Second))
	require.NoError(t, eventBus.PublishEvent(ctx, &eventing.Event[int64]{
		Message: msg.Message{
			ID:        "e4",
			Kind:      msg.KindEvent,
			Type:      "UserCreated",
			Timestamp: time.Now().Add(1 * time.Second),
			Payload:   msg.NewPayload(nil),
			Metadata:  msg.NewMetadata(),
		},
		AggregateID:   1,
		AggregateType: "user",
		Version:       4,
		SchemaVersion: 1,
	}))

	require.Eventually(t, func() bool {
		st := p2.Status()
		return st.ProcessedEvents == 4
	}, 2*time.Second, 10*time.Millisecond)

	ck2, err := cs2.Load(ctx, "proj")
	require.NoError(t, err)
	require.NotNil(t, ck2)
	require.Equal(t, int64(4), ck2.Position)
	require.Equal(t, "e4", ck2.LastEventID)
}
