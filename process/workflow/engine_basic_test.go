package workflow

import (
	"context"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	"gochen/testkit/require"

	"gochen/clock"
	"gochen/errors"
)

func TestEngine_LinearWorkflowLifecycle(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := NewEngine(store)

	definition := &Definition{
		ID:          "order-fulfillment",
		StartNodeID: "reserve_inventory",
		Nodes: []Node{
			{ID: "reserve_inventory", Edges: NewEdges("charge_payment")},
			{ID: "charge_payment", Edges: NewEdges("ship_order")},
			{ID: "ship_order"},
		},
	}

	require.NoError(t, engine.SaveDefinition(ctx, definition))
	require.NoError(t, engine.CreateInstance(ctx, ID("wf-1"), definition.ID))

	state, err := store.Get(ctx, ID("wf-1"))
	require.NoError(t, err)
	require.Equal(t, InstanceStatusPending, state.Status)
	require.Empty(t, state.ActiveNodeIDs)
	require.Len(t, state.History, 1)
	require.Equal(t, "create", state.History[0].Action)

	require.NoError(t, engine.StartInstance(ctx, ID("wf-1")))

	state, err = store.Get(ctx, ID("wf-1"))
	require.NoError(t, err)
	require.Equal(t, InstanceStatusRunning, state.Status)
	require.Equal(t, []string{"reserve_inventory"}, state.ActiveNodeIDs)
	require.Len(t, state.History, 2)
	require.Equal(t, "start", state.History[1].Action)

	require.NoError(t, engine.Advance(ctx, ID("wf-1")))

	state, err = store.Get(ctx, ID("wf-1"))
	require.NoError(t, err)
	require.Equal(t, InstanceStatusRunning, state.Status)
	require.Equal(t, []string{"charge_payment"}, state.ActiveNodeIDs)
	require.Equal(t, []string{"reserve_inventory"}, state.CompletedNodeIDs)
	require.Equal(t, "advance", state.History[2].Action)

	require.NoError(t, engine.Advance(ctx, ID("wf-1")))
	require.NoError(t, engine.Advance(ctx, ID("wf-1")))

	state, err = store.Get(ctx, ID("wf-1"))
	require.NoError(t, err)
	require.Equal(t, InstanceStatusCompleted, state.Status)
	require.Empty(t, state.ActiveNodeIDs)
	require.Equal(t, []string{"reserve_inventory", "charge_payment", "ship_order"}, state.CompletedNodeIDs)
	require.Equal(t, "complete", state.History[len(state.History)-1].Action)
	require.False(t, state.CompletedAt.IsZero())
}

func TestEngine_DirectStructUsesDefaultClockWithoutSharedMutation(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := &Engine{store: store}

	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < cap(errs); i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs <- engine.SaveDefinition(ctx, &Definition{
				ID:          "direct-engine-" + strconv.Itoa(i),
				StartNodeID: "start",
				Nodes:       []Node{{ID: "start"}},
			})
		}(i)
	}

	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
}

func TestEngine_StoreErrorsPropagate(t *testing.T) {
	ctx := context.Background()
	definition := &Definition{
		ID:          "store-error-definition",
		StartNodeID: "start",
		Nodes:       []Node{{ID: "start", Edges: NewEdges("done")}, {ID: "done"}},
	}

	t.Run("SaveDefinition lookup", func(t *testing.T) {
		engine := NewEngine(failingStore{})
		err := engine.SaveDefinition(ctx, definition)
		require.ErrorContains(t, err, "store get definition failed")
	})

	t.Run("SaveDefinition save", func(t *testing.T) {
		store := &definitionSaveFailStore{MemoryStore: NewMemoryStore()}
		engine := NewEngine(store)
		def := cloneDefinition(definition)
		def.CreatedAt = time.Date(2026, 6, 20, 9, 0, 0, 0, time.UTC)
		err := engine.SaveDefinition(ctx, def)
		require.ErrorContains(t, err, "store save definition failed")
	})

	t.Run("CreateInstance lookup", func(t *testing.T) {
		engine := NewEngine(failingStore{})
		err := engine.CreateInstance(ctx, ID("wf-create-lookup-fail"), definition.ID)
		require.ErrorContains(t, err, "store get definition failed")
	})

	t.Run("CreateInstance save", func(t *testing.T) {
		base := NewMemoryStore()
		require.NoError(t, base.SaveDefinition(ctx, definition))
		engine := NewEngine(&instanceSaveFailStore{MemoryStore: base})
		err := engine.CreateInstance(ctx, ID("wf-create-save-fail"), definition.ID)
		require.ErrorContains(t, err, "store save failed")
	})

	t.Run("StartInstance get", func(t *testing.T) {
		engine := NewEngine(failingStore{})
		err := engine.StartInstance(ctx, ID("wf-start-get-fail"))
		require.ErrorContains(t, err, "store get failed")
	})

	t.Run("StartInstance save", func(t *testing.T) {
		base := NewMemoryStore()
		setup := NewEngine(base)
		require.NoError(t, setup.SaveDefinition(ctx, definition))
		require.NoError(t, setup.CreateInstance(ctx, ID("wf-start-save-fail"), definition.ID))
		engine := NewEngine(&instanceSaveFailStore{MemoryStore: base})
		err := engine.StartInstance(ctx, ID("wf-start-save-fail"))
		require.ErrorContains(t, err, "store save failed")
	})

	t.Run("Advance get", func(t *testing.T) {
		engine := NewEngine(failingStore{})
		err := engine.Advance(ctx, ID("wf-advance-get-fail"))
		require.ErrorContains(t, err, "store get failed")
	})

	t.Run("Advance save", func(t *testing.T) {
		base := NewMemoryStore()
		setup := NewEngine(base)
		require.NoError(t, setup.SaveDefinition(ctx, definition))
		require.NoError(t, setup.CreateInstance(ctx, ID("wf-advance-save-fail"), definition.ID))
		require.NoError(t, setup.StartInstance(ctx, ID("wf-advance-save-fail")))
		engine := NewEngine(&instanceSaveFailStore{MemoryStore: base})
		err := engine.Advance(ctx, ID("wf-advance-save-fail"))
		require.ErrorContains(t, err, "store save failed")
	})
}

func TestEngine_FailedInstanceIsNotRunning(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := NewEngine(store)
	definition := &Definition{
		ID:          "failed-status-definition",
		StartNodeID: "start",
		Nodes:       []Node{{ID: "start"}},
	}
	require.NoError(t, engine.SaveDefinition(ctx, definition))
	require.NoError(t, store.Save(ctx, &State{
		ID:               ID("wf-failed"),
		DefinitionID:     definition.ID,
		Status:           InstanceStatusFailed,
		ActiveNodeIDs:    []string{"start"},
		CompletedNodeIDs: []string{},
		History:          []HistoryEntry{{Action: "failed", NodeID: "start"}},
	}))

	err := engine.Advance(ctx, ID("wf-failed"))
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Conflict))
	require.ErrorContains(t, err, "workflow instance is not running")
}

func TestEngine_updateInstanceData_ReturnsGetError(t *testing.T) {
	ctx := context.Background()
	engine := NewEngine(failingStore{})

	err := engine.updateInstanceData(ctx, ID("wf-4"), func(data map[string]any) error { return nil })
	require.Error(t, err)
}

func TestEngine_updateInstanceDataUsesOptimisticVersionSave(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := NewEngine(store)

	require.NoError(t, engine.SaveDefinition(ctx, &Definition{
		ID:          "data-update",
		StartNodeID: "node",
		Nodes:       []Node{{ID: "node"}},
	}))
	require.NoError(t, engine.CreateInstance(ctx, ID("wf-data-update"), "data-update"))

	err := engine.updateInstanceData(ctx, ID("wf-data-update"), func(data map[string]any) error {
		data["status"] = "updated"
		return nil
	})
	require.NoError(t, err)

	state, err := store.Get(ctx, ID("wf-data-update"))
	require.NoError(t, err)
	require.Equal(t, uint64(2), state.Version)
	require.Equal(t, "updated", state.Data["status"])
}

func TestEngine_DoesNotExposeStateMutationBypass(t *testing.T) {
	method, ok := reflect.TypeOf(&Engine{}).MethodByName("HandleCommand")
	require.False(t, ok)
	require.Zero(t, method)
}

func TestEngine_NodeTimeoutDetection(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	manualClock := clock.NewManualClock(time.Date(2026, 8, 25, 10, 0, 0, 0, time.UTC))
	engine := NewEngine(store).WithClock(manualClock)

	def := &Definition{
		ID:          "timeout-def",
		StartNodeID: "timed_task",
		Nodes: []Node{
			{ID: "timed_task", Timeout: 30 * time.Minute, Edges: NewEdges("end")},
			{ID: "end"},
		},
	}
	require.NoError(t, engine.SaveDefinition(ctx, def))
	require.NoError(t, engine.CreateInstance(ctx, ID("wf-timeout"), "timeout-def"))
	require.NoError(t, engine.StartInstance(ctx, ID("wf-timeout")))

	// 此时未超时（经过 10 分钟）
	manualClock.Advance(10 * time.Minute)
	timedOut, err := engine.CheckTimeouts(ctx, ID("wf-timeout"))
	require.NoError(t, err)
	require.Empty(t, timedOut)

	// 推进 25 分钟（累计 35 分钟，超过 30 分钟）
	manualClock.Advance(25 * time.Minute)
	timedOut, err = engine.CheckTimeouts(ctx, ID("wf-timeout"))
	require.NoError(t, err)
	require.Equal(t, []string{"timed_task"}, timedOut)
}

func TestEngine_GlobalInterceptors(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := NewEngine(store)

	var interceptedActions []ActionType
	engine.Use(func(c context.Context, tctx *TransitionContext, next func(context.Context) error) error {
		interceptedActions = append(interceptedActions, tctx.Action)
		return next(c)
	})

	def := &Definition{
		ID:          "interceptor-def",
		StartNodeID: "start",
		Nodes: []Node{
			{ID: "start", Edges: NewEdges("end")},
			{ID: "end"},
		},
	}
	require.NoError(t, engine.SaveDefinition(ctx, def))
	require.NoError(t, engine.CreateInstance(ctx, ID("wf-interceptor"), "interceptor-def"))
	require.NoError(t, engine.StartInstance(ctx, ID("wf-interceptor")))
	require.NoError(t, engine.AdvanceNode(ctx, ID("wf-interceptor"), "start"))

	require.Equal(t, []ActionType{ActionCreate, ActionStart, ActionAdvance}, interceptedActions)
}
