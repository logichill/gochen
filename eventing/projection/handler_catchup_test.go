package projection

import (
	"context"
	"sync"
	"testing"
	"time"

	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/internal/testutil"
	"gochen/eventing/registry"
	"gochen/eventing/store"
	"gochen/eventing/upcast"
	"gochen/testkit/require"
)

// flakyStreamEventStore 包装 MemoryEventStore：在前 failRemaining 次 StreamEvents
// 调用返回 failErr（默认 Database 错误），用于模拟存储瞬断。
type flakyStreamEventStore[ID comparable] struct {
	*store.MemoryEventStore[ID]

	mu            sync.Mutex
	failRemaining int
	failErr       error
}

func newFlakyStreamEventStore[ID comparable](failRemaining int) *flakyStreamEventStore[ID] {
	return &flakyStreamEventStore[ID]{
		MemoryEventStore: store.NewMemoryEventStore[ID](),
		failRemaining:    failRemaining,
		failErr: errors.NewCode(errors.Database, "event store temporarily unavailable").
			WithContext("flaky", true),
	}
}

func (s *flakyStreamEventStore[ID]) StreamEvents(ctx context.Context, opts *store.StreamOptions) (*store.StreamResult[ID], error) {
	s.mu.Lock()
	if s.failRemaining > 0 {
		s.failRemaining--
		s.mu.Unlock()
		return nil, s.failErr
	}
	s.mu.Unlock()
	return s.MemoryEventStore.StreamEvents(ctx, opts)
}

// newCatchUpTestManager 构造带 checkpoint 存储的投影管理器（默认配置，
// CatchUpMinInterval=30s）并启动投影，返回运行期与 handler。
func newCatchUpTestManager(t *testing.T, eventStore store.IEventStreamReader[int64], name string) (*ProjectionManager[int64], *projectionRuntime[int64], *projectionEventHandler[int64]) {
	t.Helper()
	reg := registry.NewRegistry()
	require.NoError(t, reg.Register("A", func() any { return &struct{}{} }))
	manager, err := NewProjectionManager[int64](eventStore, &MockEventBus{}, reg, upcast.NewUpgraderRegistry())
	require.NoError(t, err)
	manager, err = manager.WithCheckpointStore(NewMemoryCheckpointStore())
	require.NoError(t, err)

	projection := NewMockProjection(name, []string{"A"})
	require.NoError(t, manager.RegisterProjection(projection))
	require.NoError(t, manager.StartProjection(projection.Name()))

	rt, ok := manager.runtime(name)
	require.True(t, ok)
	handler := rt.handlers["A"]
	require.NotNil(t, handler)
	return manager, rt, handler
}

// newTypedEvent 构造聚合 ID=1、类型 A、版本与全局位置均为 pos 的测试事件。
func newTypedEvent(pos int64) *eventing.Event[int64] {
	e := testutil.NewEvent[int64](1, "Agg", "A", uint64(pos), &struct{}{})
	e.GlobalPosition = pos
	return e
}

// P1-3 回归：追赶遇非 NotFound 存储错误时不得置终态 error。
// 第一次在线事件追赶失败（返回错误），第二次在线事件必须立即重试追赶并恢复，
// 事件照常处理；状态保持 running，绝不静默丢弃。
func TestHandleEventCatchUpStoreErrorRecoversOnNextEvent(t *testing.T) {
	ctx := context.Background()
	eventStore := newFlakyStreamEventStore[int64](1)
	_, rt, handler := newCatchUpTestManager(t, eventStore, "recover-a")

	a1 := newTypedEvent(1)
	a2 := newTypedEvent(2)
	a3 := newTypedEvent(3)
	require.NoError(t, eventStore.AppendEvents(ctx, "Agg", 1, toStorableEvents([]eventing.Event[int64]{*a1, *a2, *a3}), 0))

	// 第一次：冷启动追赶撞上存储错误 → 返回错误（对外可见），不置终态。
	firstErr := handler.HandleEvent(ctx, a1)
	require.Error(t, firstErr)
	require.False(t, errors.Is(firstErr, errors.NotFound))
	status := rt.statusCopy()
	require.Equal(t, "running", status.Status, "catch-up failure must not move runtime into terminal error state")
	require.True(t, rt.hasPendingCatchUp(), "failed catch-up must be marked pending for immediate retry")

	// 第二次：存储已恢复 → 立即重试追赶成功，未重放区间（a1..a3）全部补齐。
	require.NoError(t, handler.HandleEvent(ctx, a2))
	proj := rt.projection.(*MockProjection)
	require.Equal(t, 3, proj.processedEvents, "missed events must be replayed after recovery")

	status = rt.statusCopy()
	require.Equal(t, "running", status.Status)
	require.False(t, rt.hasPendingCatchUp(), "pending flag must clear after successful catch-up")
	require.NotNil(t, rt.cursorSnapshot())
	require.Equal(t, int64(3), rt.cursorSnapshot().Position)

	// 后续事件照常在线处理。
	require.NoError(t, handler.HandleEvent(ctx, a3))
	require.Equal(t, 3, rt.projection.(*MockProjection).processedEvents, "replayed event must be idempotently skipped")
}

// P2#13 回归：已确认的真实空洞不得被节流窗口抑制。
// 即便刚刚发生过一次追赶（窗口激活），确认存在的未处理事件也必须被立即
// 追赶补齐，游标不得越进把空洞事件永久埋没。
func TestHandleEventConfirmedRealGapNotSuppressedByThrottle(t *testing.T) {
	ctx := context.Background()
	eventStore := store.NewMemoryEventStore[int64]()
	manager, rt, handler := newCatchUpTestManager(t, eventStore, "gap-a")

	a1 := newTypedEvent(1)
	a2 := newTypedEvent(2)
	a3 := newTypedEvent(3)
	require.NoError(t, eventStore.AppendEvents(ctx, "Agg", 1, toStorableEvents([]eventing.Event[int64]{*a1, *a2, *a3}), 0))

	// 游标在 a1，且刚刚发生过一次追赶（节流窗口激活）。
	rt.prefillFromCheckpoint(NewCheckpoint("gap-a", 1, a1.ID, a1.Timestamp))
	require.NoError(t, manager.StartProjection("gap-a"))
	rt.markCatchUp(time.Now())

	// a2 只写入存储、不经总线分发（模拟总线漏投）；a3 在线到达。
	// 旧逻辑：hasRealGap 确认真实空洞后被 30s 窗口抑制，a3 在线处理后游标
	// 越进到 3，a2 被永久埋没；新逻辑：确认的真实空洞绕过窗口，立即追赶补齐。
	require.NoError(t, handler.HandleEvent(ctx, a3))

	proj := rt.projection.(*MockProjection)
	require.Equal(t, 2, proj.processedEvents, "confirmed real gap must be caught up even inside throttle window")
	require.NotNil(t, rt.cursorSnapshot())
	require.Equal(t, int64(3), rt.cursorSnapshot().Position)
}

// 未确认的空洞探测（存储瞬断导致查询失败）仍受节流窗口约束：
// 窗口内不发起追赶，事件照常在线处理，也不得误标追赶失败。
func TestHandleEventUnconfirmedGapStillThrottled(t *testing.T) {
	ctx := context.Background()
	// 让所有 StreamEvents 调用失败：hasRealGap 探测必然失败（未确认）。
	eventStore := newFlakyStreamEventStore[int64](100)
	manager, rt, handler := newCatchUpTestManager(t, eventStore, "probe-a")

	a1 := newTypedEvent(1)
	a2 := newTypedEvent(2)
	a3 := newTypedEvent(3)
	require.NoError(t, eventStore.AppendEvents(ctx, "Agg", 1, toStorableEvents([]eventing.Event[int64]{*a1, *a2, *a3}), 0))

	rt.prefillFromCheckpoint(NewCheckpoint("probe-a", 1, a1.ID, a1.Timestamp))
	require.NoError(t, manager.StartProjection("probe-a"))
	catchUpAt := time.Now().Add(-time.Second)
	rt.markCatchUp(catchUpAt)

	// a3 在线到达：位置差命中空洞探测但探测失败（未确认）→ 窗口内抑制追赶。
	require.NoError(t, handler.HandleEvent(ctx, a3))

	proj := rt.projection.(*MockProjection)
	require.Equal(t, 1, proj.processedEvents, "unconfirmed gap must be throttled: only the live event is processed")
	require.False(t, rt.hasPendingCatchUp(), "throttled probe must not mark catch-up pending")
	status := rt.statusCopy()
	require.Equal(t, "running", status.Status)
}
