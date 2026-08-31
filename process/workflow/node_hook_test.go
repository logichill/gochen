package workflow

import (
	"context"
	"testing"
	"time"

	"gochen/clock"
	"gochen/errors"
	"gochen/testkit/require"
)

// hookTrace 记录钩子触发顺序，便于断言时机与作用域。
type hookTrace struct {
	entries []string
}

func (h *hookTrace) record(hctx *NodeHookContext) {
	h.entries = append(h.entries, string(hctx.Phase)+":"+hctx.NodeID+":"+string(hctx.Action))
}

func linearHookDefinition() *Definition {
	return &Definition{
		ID:          "hook-linear",
		StartNodeID: "a",
		Nodes: []Node{
			{ID: "a", Edges: []Edge{NewEdge("b")}},
			{ID: "b", Edges: []Edge{NewEdge("c")}},
			{ID: "c"},
		},
	}
}

func TestNodeHooks_EnterExitOrder(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := NewEngine(store)

	trace := &hookTrace{}
	engine.OnNodeEnter(NodeAnyID, func(_ context.Context, hctx *NodeHookContext) error {
		trace.record(hctx)
		return nil
	})
	engine.OnNodeExit(NodeAnyID, func(_ context.Context, hctx *NodeHookContext) error {
		trace.record(hctx)
		return nil
	})

	require.NoError(t, engine.SaveDefinition(ctx, linearHookDefinition()))
	require.NoError(t, engine.CreateInstance(ctx, ID("h1"), "hook-linear"))
	require.NoError(t, engine.StartInstance(ctx, ID("h1")))
	require.NoError(t, engine.AdvanceNode(ctx, ID("h1"), "a"))
	require.NoError(t, engine.AdvanceNode(ctx, ID("h1"), "b"))

	require.Equal(t, []string{
		"enter:a:start",
		"exit:a:advance",
		"enter:b:advance",
		"exit:b:advance",
		"enter:c:advance",
	}, trace.entries)
}

func TestNodeHooks_ScopedToNodeID(t *testing.T) {
	ctx := context.Background()
	engine := NewEngine(NewMemoryStore())

	var entered []string
	engine.OnNodeEnter("b", func(_ context.Context, hctx *NodeHookContext) error {
		entered = append(entered, hctx.NodeID)
		return nil
	})

	require.NoError(t, engine.SaveDefinition(ctx, linearHookDefinition()))
	require.NoError(t, engine.CreateInstance(ctx, ID("h2"), "hook-linear"))
	require.NoError(t, engine.StartInstance(ctx, ID("h2")))
	require.NoError(t, engine.AdvanceNode(ctx, ID("h2"), "a"))
	require.NoError(t, engine.AdvanceNode(ctx, ID("h2"), "b"))

	// 只注册了 b 的钩子，a / c 的激活不应触发。
	require.Equal(t, []string{"b"}, entered)
}

func TestNodeHooks_ErrorAbortsTransition(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := NewEngine(store)

	engine.OnNodeEnter("b", func(_ context.Context, _ *NodeHookContext) error {
		return errors.NewCode(errors.InvalidInput, "reject entering b")
	})

	require.NoError(t, engine.SaveDefinition(ctx, linearHookDefinition()))
	require.NoError(t, engine.CreateInstance(ctx, ID("h3"), "hook-linear"))
	require.NoError(t, engine.StartInstance(ctx, ID("h3")))

	before, err := store.Get(ctx, ID("h3"))
	require.NoError(t, err)

	err = engine.AdvanceNode(ctx, ID("h3"), "a")
	require.Error(t, err)
	require.ErrorContains(t, err, "node hook failed")

	// 钩子否决后实例必须保持推进前状态，不能出现半推进。
	after, err := store.Get(ctx, ID("h3"))
	require.NoError(t, err)
	require.Equal(t, before.Version, after.Version)
	require.Equal(t, []string{"a"}, after.ActiveNodeIDs)
	require.Equal(t, InstanceStatusRunning, after.Status)
}

func TestNodeHooks_DataIsSnapshot(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := NewEngine(store)

	engine.OnNodeExit("a", func(_ context.Context, hctx *NodeHookContext) error {
		require.Equal(t, "original", hctx.Data["field"])
		// 钩子改写快照不应影响实例状态。
		hctx.Data["field"] = "mutated by hook"
		hctx.Data["injected"] = true
		return nil
	})

	require.NoError(t, engine.SaveDefinition(ctx, linearHookDefinition()))
	require.NoError(t, engine.CreateInstanceWithData(ctx, ID("h4"), "hook-linear", map[string]any{"field": "original"}))
	require.NoError(t, engine.StartInstance(ctx, ID("h4")))
	require.NoError(t, engine.AdvanceNode(ctx, ID("h4"), "a"))

	st, err := store.Get(ctx, ID("h4"))
	require.NoError(t, err)
	require.Equal(t, "original", st.Data["field"])
	require.NotContains(t, st.Data, "injected")
}

func TestNodeHooks_RejectFiresExitAndEnter(t *testing.T) {
	ctx := context.Background()
	engine := NewEngine(NewMemoryStore())

	trace := &hookTrace{}
	engine.OnNodeEnter(NodeAnyID, func(_ context.Context, hctx *NodeHookContext) error {
		trace.record(hctx)
		return nil
	})
	engine.OnNodeExit(NodeAnyID, func(_ context.Context, hctx *NodeHookContext) error {
		trace.record(hctx)
		return nil
	})

	def := &Definition{
		ID:          "hook-reject",
		StartNodeID: "submit",
		Nodes: []Node{
			{ID: "submit", Edges: []Edge{NewEdge("review")}},
			{ID: "review", RejectTo: []string{"submit"}},
		},
	}
	require.NoError(t, engine.SaveDefinition(ctx, def))
	require.NoError(t, engine.CreateInstance(ctx, ID("h5"), "hook-reject"))
	require.NoError(t, engine.StartInstance(ctx, ID("h5")))
	require.NoError(t, engine.AdvanceNode(ctx, ID("h5"), "submit"))
	require.NoError(t, engine.RejectNode(ctx, ID("h5"), "review", "submit"))

	require.Equal(t, []string{
		"enter:submit:start",
		"exit:submit:advance",
		"enter:review:advance",
		"exit:review:reject",
		"enter:submit:reject",
	}, trace.entries)
}

func TestNodeHooks_TerminateFiresExitForActiveNodes(t *testing.T) {
	ctx := context.Background()
	engine := NewEngine(NewMemoryStore())

	var exited []string
	engine.OnNodeExit(NodeAnyID, func(_ context.Context, hctx *NodeHookContext) error {
		if hctx.Action == ActionTerminate {
			exited = append(exited, hctx.NodeID)
		}
		return nil
	})

	require.NoError(t, engine.SaveDefinition(ctx, linearHookDefinition()))
	require.NoError(t, engine.CreateInstance(ctx, ID("h6"), "hook-linear"))
	require.NoError(t, engine.StartInstance(ctx, ID("h6")))
	require.NoError(t, engine.TerminateInstance(ctx, ID("h6"), "manual stop"))

	require.Equal(t, []string{"a"}, exited)
}

func TestNodeHooks_JoinEntersOnceWhenReady(t *testing.T) {
	ctx := context.Background()
	engine := NewEngine(NewMemoryStore())

	var entered []string
	engine.OnNodeEnter("merge", func(_ context.Context, hctx *NodeHookContext) error {
		entered = append(entered, hctx.FromNodeID)
		return nil
	})

	def := &Definition{
		ID:          "hook-join",
		StartNodeID: "fork",
		Nodes: []Node{
			{ID: "fork", Kind: NodeKindBranch, Edges: []Edge{NewEdge("left"), NewEdge("right")}},
			{ID: "left", Edges: []Edge{NewEdge("merge")}},
			{ID: "right", Edges: []Edge{NewEdge("merge")}},
			{ID: "merge", Kind: NodeKindJoin},
		},
	}
	require.NoError(t, engine.SaveDefinition(ctx, def))
	require.NoError(t, engine.CreateInstance(ctx, ID("h7"), "hook-join"))
	require.NoError(t, engine.StartInstance(ctx, ID("h7")))
	require.NoError(t, engine.AdvanceNode(ctx, ID("h7"), "fork"))

	// 第一条分支到达只是 join_wait，不应触发进入钩子。
	require.NoError(t, engine.AdvanceNode(ctx, ID("h7"), "left"))
	require.Empty(t, entered)

	// 第二条分支到齐后 join 激活，钩子只触发一次。
	require.NoError(t, engine.AdvanceNode(ctx, ID("h7"), "right"))
	require.Equal(t, []string{"right"}, entered)
}

func TestNodeHooks_InterceptorWrapsNodeHooks(t *testing.T) {
	ctx := context.Background()
	engine := NewEngine(NewMemoryStore())

	var order []string
	engine.Use(func(c context.Context, _ *TransitionContext, next func(context.Context) error) error {
		order = append(order, "interceptor:before")
		err := next(c)
		order = append(order, "interceptor:after")
		return err
	})
	engine.OnNodeExit("a", func(_ context.Context, _ *NodeHookContext) error {
		order = append(order, "hook:exit:a")
		return nil
	})

	require.NoError(t, engine.SaveDefinition(ctx, linearHookDefinition()))
	require.NoError(t, engine.CreateInstance(ctx, ID("h8"), "hook-linear"))
	require.NoError(t, engine.StartInstance(ctx, ID("h8")))
	order = nil
	require.NoError(t, engine.AdvanceNode(ctx, ID("h8"), "a"))

	require.Equal(t, []string{"interceptor:before", "hook:exit:a", "interceptor:after"}, order)
}

func TestEngine_ScanTimeouts(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	base := time.Date(2026, 8, 25, 10, 0, 0, 0, time.UTC)
	fake := clock.NewManualClock(base)
	engine := NewEngine(store).WithClock(fake)

	def := &Definition{
		ID:          "timeout-scan",
		StartNodeID: "wait",
		Nodes: []Node{
			{ID: "wait", Timeout: time.Hour, Edges: []Edge{NewEdge("done")}},
			{ID: "done"},
		},
	}
	require.NoError(t, engine.SaveDefinition(ctx, def))

	require.NoError(t, engine.CreateInstance(ctx, ID("slow"), "timeout-scan"))
	require.NoError(t, engine.StartInstance(ctx, ID("slow")))

	// 尚未超时。
	found, err := engine.ScanTimeouts(ctx, 10)
	require.NoError(t, err)
	require.Empty(t, found)

	// 推进时钟越过节点超时时限后，巡检应能"发现"该实例，无需事先知道实例 ID。
	fake.Advance(2 * time.Hour)
	found, err = engine.ScanTimeouts(ctx, 10)
	require.NoError(t, err)
	require.Len(t, found, 1)
	require.Equal(t, ID("slow"), found[0].InstanceID)
	require.Equal(t, []string{"wait"}, found[0].NodeIDs)
}

func TestEngine_ScanTimeoutsUnsupportedStore(t *testing.T) {
	ctx := context.Background()
	engine := NewEngine(&nonQueryableStore{IStore: NewMemoryStore()})

	_, err := engine.ScanTimeouts(ctx, 10)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Unsupported))
}

// nonQueryableStore 只嵌入 IStore 接口，因此方法集里没有 ListInstances，
// 用于验证仅实现最小存储契约时 ScanTimeouts 返回 Unsupported。
type nonQueryableStore struct {
	IStore
}

func TestMemoryStore_ListInstances(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	base := time.Date(2026, 8, 25, 10, 0, 0, 0, time.UTC)

	require.NoError(t, store.Save(ctx, &State{
		ID: "newer", DefinitionID: "d1", Status: InstanceStatusRunning,
		ActiveNodeIDs:       []string{"n"},
		ActiveNodeStartedAt: map[string]time.Time{"n": base.Add(time.Hour)},
	}))
	require.NoError(t, store.Save(ctx, &State{
		ID: "older", DefinitionID: "d1", Status: InstanceStatusRunning,
		ActiveNodeIDs:       []string{"n"},
		ActiveNodeStartedAt: map[string]time.Time{"n": base},
	}))
	require.NoError(t, store.Save(ctx, &State{
		ID: "done", DefinitionID: "d1", Status: InstanceStatusCompleted,
	}))
	require.NoError(t, store.Save(ctx, &State{
		ID: "other", DefinitionID: "d2", Status: InstanceStatusRunning,
		ActiveNodeIDs:       []string{"n"},
		ActiveNodeStartedAt: map[string]time.Time{"n": base},
	}))

	// 按状态过滤，且最早活动的实例排在前面。
	got, err := store.ListInstances(ctx, InstanceQuery{
		Statuses: []InstanceStatus{InstanceStatusRunning},
		Limit:    10,
	})
	require.NoError(t, err)
	require.Len(t, got, 3)
	require.Equal(t, ID("older"), got[0].ID)

	// 按定义过滤。
	got, err = store.ListInstances(ctx, InstanceQuery{DefinitionID: "d2", Limit: 10})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, ID("other"), got[0].ID)

	// ActiveSince 只保留活动时刻不晚于给定时间的实例。
	got, err = store.ListInstances(ctx, InstanceQuery{
		Statuses:    []InstanceStatus{InstanceStatusRunning},
		ActiveSince: base,
		Limit:       10,
	})
	require.NoError(t, err)
	require.Len(t, got, 2)

	// Limit 生效。
	got, err = store.ListInstances(ctx, InstanceQuery{Limit: 1})
	require.NoError(t, err)
	require.Len(t, got, 1)

	// 非法 limit 必须报错，避免调用方无意做全表扫描。
	_, err = store.ListInstances(ctx, InstanceQuery{Limit: 0})
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
}
