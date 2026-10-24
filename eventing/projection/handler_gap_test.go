package projection

import (
	"context"
	"testing"

	"gochen/eventing"
	"gochen/eventing/internal/testutil"
	"gochen/eventing/registry"
	"gochen/eventing/store"
	"gochen/eventing/upcast"
	"gochen/testkit/require"
)

// 跨事件类型的全局位置差不应触发伪追赶：投影只订阅 A 类型时，
// 中间的 B 类型事件不构成该投影的空洞。
func TestHandleEventDoesNotCatchUpOnCrossTypePositionGap(t *testing.T) {
	ctx := context.Background()
	eventStore := store.NewMemoryEventStore[int64]()
	reg := registry.NewRegistry()
	require.NoError(t, reg.Register("A", func() any { return &struct{}{} }))
	require.NoError(t, reg.Register("B", func() any { return &struct{}{} }))

	manager, err := NewProjectionManager[int64](eventStore, &MockEventBus{}, reg, upcast.NewUpgraderRegistry())
	require.NoError(t, err)
	manager, err = manager.WithCheckpointStore(NewMemoryCheckpointStore())
	require.NoError(t, err)

	projection := NewMockProjection("only-a", []string{"A"})
	require.NoError(t, manager.RegisterProjection(projection))
	require.NoError(t, manager.StartProjection(projection.Name()))

	rt, ok := manager.runtime(projection.Name())
	require.True(t, ok)

	// 预置游标：位置 1 已处理，LastEventID 指向 A#1。
	a1 := testutil.NewEvent[int64](1, "Agg", "A", 1, &struct{}{})
	a1.GlobalPosition = 1
	require.NoError(t, eventStore.AppendEvents(ctx, "Agg", 1, toStorableEvents([]eventing.Event[int64]{*a1}), 0))
	rt.prefillFromCheckpoint(NewCheckpoint(projection.Name(), 1, a1.ID, a1.Timestamp))

	// 在线 B 事件位置 5：位置差 4，但 B 不在订阅范围，不应触发追赶。
	b := testutil.NewEvent[int64](2, "Agg", "B", 1, &struct{}{})
	b.GlobalPosition = 5
	handler := rt.handlers["A"]
	require.NotNil(t, handler)
	require.NoError(t, handler.HandleEvent(ctx, b))
	require.True(t, rt.lastCatchUpAt.IsZero(), "cross-type position gap must not trigger catch-up")
}

// 存在本投影支持类型的未处理事件时，位置差构成真实空洞，必须追赶。
func TestHandleEventCatchUpOnRealGap(t *testing.T) {
	ctx := context.Background()
	eventStore := store.NewMemoryEventStore[int64]()
	reg := registry.NewRegistry()
	require.NoError(t, reg.Register("A", func() any { return &struct{}{} }))

	manager, err := NewProjectionManager[int64](eventStore, &MockEventBus{}, reg, upcast.NewUpgraderRegistry())
	require.NoError(t, err)
	manager, err = manager.WithCheckpointStore(NewMemoryCheckpointStore())
	require.NoError(t, err)

	projection := NewMockProjection("gap-a", []string{"A"})
	require.NoError(t, manager.RegisterProjection(projection))
	require.NoError(t, manager.StartProjection(projection.Name()))

	rt, ok := manager.runtime(projection.Name())
	require.True(t, ok)

	a1 := testutil.NewEvent[int64](1, "Agg", "A", 1, &struct{}{})
	a1.GlobalPosition = 1
	a2 := testutil.NewEvent[int64](1, "Agg", "A", 2, &struct{}{})
	a2.GlobalPosition = 2
	require.NoError(t, eventStore.AppendEvents(ctx, "Agg", 1, toStorableEvents([]eventing.Event[int64]{*a1, *a2}), 0))
	rt.prefillFromCheckpoint(NewCheckpoint(projection.Name(), 1, a1.ID, a1.Timestamp))

	// 在线 A 事件位置 3：中间存在未处理的 A#2 → 真实空洞，应触发追赶并重放 A#2。
	a3 := testutil.NewEvent[int64](1, "Agg", "A", 3, &struct{}{})
	a3.GlobalPosition = 3
	handler := rt.handlers["A"]
	require.NotNil(t, handler)
	require.NoError(t, handler.HandleEvent(ctx, a3))
	require.False(t, rt.lastCatchUpAt.IsZero(), "real gap must trigger catch-up")
}
