package workflow

import (
	"context"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

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
			{ID: "reserve_inventory", Next: []string{"charge_payment"}},
			{ID: "charge_payment", Next: []string{"ship_order"}},
			{ID: "ship_order"},
		},
	}

	require.NoError(t, engine.SaveDefinition(ctx, definition))
	require.NoError(t, engine.CreateInstance(ctx, ID("wf-1"), definition.ID))

	state, err := store.Get(ctx, ID("wf-1"))
	require.NoError(t, err)
	require.Equal(t, InstanceStatusPending, state.Status)
	require.Empty(t, state.CurrentNodeID)
	require.Empty(t, state.ActiveNodeIDs)
	require.Len(t, state.History, 1)
	require.Equal(t, "create", state.History[0].Action)

	require.NoError(t, engine.StartInstance(ctx, ID("wf-1")))

	state, err = store.Get(ctx, ID("wf-1"))
	require.NoError(t, err)
	require.Equal(t, InstanceStatusRunning, state.Status)
	require.Equal(t, []string{"reserve_inventory"}, state.ActiveNodeIDs)
	require.Equal(t, "reserve_inventory", state.CurrentNodeID)
	require.Len(t, state.History, 2)
	require.Equal(t, "start", state.History[1].Action)

	require.NoError(t, engine.Advance(ctx, ID("wf-1")))

	state, err = store.Get(ctx, ID("wf-1"))
	require.NoError(t, err)
	require.Equal(t, InstanceStatusRunning, state.Status)
	require.Equal(t, []string{"charge_payment"}, state.ActiveNodeIDs)
	require.Equal(t, "charge_payment", state.CurrentNodeID)
	require.Equal(t, []string{"reserve_inventory"}, state.CompletedNodeIDs)
	require.Equal(t, "advance", state.History[2].Action)

	require.NoError(t, engine.Advance(ctx, ID("wf-1")))
	require.NoError(t, engine.Advance(ctx, ID("wf-1")))

	state, err = store.Get(ctx, ID("wf-1"))
	require.NoError(t, err)
	require.Equal(t, InstanceStatusCompleted, state.Status)
	require.Empty(t, state.CurrentNodeID)
	require.Empty(t, state.ActiveNodeIDs)
	require.Equal(t, []string{"reserve_inventory", "charge_payment", "ship_order"}, state.CompletedNodeIDs)
	require.Equal(t, "complete", state.History[len(state.History)-1].Action)
	require.False(t, state.CompletedAt.IsZero())
}

func TestEngine_BranchJoinLifecycle(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := NewEngine(store)

	definition := &Definition{
		ID:          "branching",
		StartNodeID: "prepare",
		Nodes: []Node{
			{ID: "prepare", Next: []string{"split"}},
			{ID: "split", Next: []string{"email", "invoice"}},
			{ID: "email", Next: []string{"join"}},
			{ID: "invoice", Next: []string{"join"}},
			{ID: "join", Next: []string{"archive"}},
			{ID: "archive"},
		},
	}

	require.NoError(t, engine.SaveDefinition(ctx, definition))
	require.NoError(t, engine.CreateInstance(ctx, ID("wf-branch"), definition.ID))
	require.NoError(t, engine.StartInstance(ctx, ID("wf-branch")))

	state, err := store.Get(ctx, ID("wf-branch"))
	require.NoError(t, err)
	require.Equal(t, []string{"prepare"}, state.ActiveNodeIDs)
	require.Equal(t, "prepare", state.CurrentNodeID)

	require.NoError(t, engine.AdvanceNode(ctx, ID("wf-branch"), "prepare"))

	state, err = store.Get(ctx, ID("wf-branch"))
	require.NoError(t, err)
	require.Equal(t, []string{"split"}, state.ActiveNodeIDs)
	require.Equal(t, []string{"prepare"}, state.CompletedNodeIDs)

	require.NoError(t, engine.AdvanceNode(ctx, ID("wf-branch"), "split"))

	state, err = store.Get(ctx, ID("wf-branch"))
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"email", "invoice"}, state.ActiveNodeIDs)
	require.Empty(t, state.CurrentNodeID)
	require.Equal(t, []string{"prepare", "split"}, state.CompletedNodeIDs)
	require.Equal(t, "branch", state.History[4].Action)

	require.NoError(t, engine.AdvanceNode(ctx, ID("wf-branch"), "email"))

	state, err = store.Get(ctx, ID("wf-branch"))
	require.NoError(t, err)
	require.Equal(t, []string{"invoice"}, state.ActiveNodeIDs)
	require.Len(t, state.PendingJoins, 1)
	require.Equal(t, "join", state.PendingJoins[0].NodeID)
	require.Equal(t, []string{"email"}, state.PendingJoins[0].ArrivedFrom)
	require.Equal(t, "join_wait", state.History[len(state.History)-1].Action)

	require.NoError(t, engine.AdvanceNode(ctx, ID("wf-branch"), "invoice"))

	state, err = store.Get(ctx, ID("wf-branch"))
	require.NoError(t, err)
	require.Equal(t, []string{"join"}, state.ActiveNodeIDs)
	require.Empty(t, state.PendingJoins)
	require.Equal(t, "join", state.CurrentNodeID)
	require.Equal(t, []string{"prepare", "split", "email", "invoice"}, state.CompletedNodeIDs)
	require.Equal(t, "join_ready", state.History[len(state.History)-1].Action)

	require.NoError(t, engine.AdvanceNode(ctx, ID("wf-branch"), "join"))

	state, err = store.Get(ctx, ID("wf-branch"))
	require.NoError(t, err)
	require.Equal(t, []string{"archive"}, state.ActiveNodeIDs)
	require.Equal(t, []string{"prepare", "split", "email", "invoice", "join"}, state.CompletedNodeIDs)

	require.NoError(t, engine.AdvanceNode(ctx, ID("wf-branch"), "archive"))

	state, err = store.Get(ctx, ID("wf-branch"))
	require.NoError(t, err)
	require.Equal(t, InstanceStatusCompleted, state.Status)
	require.Empty(t, state.ActiveNodeIDs)
	require.Empty(t, state.PendingJoins)
	require.Equal(t, []string{"prepare", "split", "email", "invoice", "join", "archive"}, state.CompletedNodeIDs)
	require.Equal(t, "complete", state.History[len(state.History)-1].Action)
}

func TestEngine_AdvanceNodeToSelectsSingleSuccessor(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := NewEngine(store)

	require.NoError(t, engine.SaveDefinition(ctx, &Definition{
		ID:          "approval-choice",
		StartNodeID: "review",
		Nodes: []Node{
			{ID: "review", Next: []string{"approved", "rejected"}},
			{ID: "approved"},
			{ID: "rejected"},
		},
	}))
	require.NoError(t, engine.CreateInstance(ctx, ID("wf-choice"), "approval-choice"))
	require.NoError(t, engine.StartInstance(ctx, ID("wf-choice")))
	require.NoError(t, engine.AdvanceNodeTo(ctx, ID("wf-choice"), "review", "approved"))

	state, err := store.Get(ctx, ID("wf-choice"))
	require.NoError(t, err)
	require.Equal(t, []string{"approved"}, state.ActiveNodeIDs)
	require.Equal(t, []string{"review"}, state.CompletedNodeIDs)
}

func TestEngine_AdvanceNodeToRejectsEmptySuccessor(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := NewEngine(store)

	require.NoError(t, engine.SaveDefinition(ctx, &Definition{
		ID:          "approval-choice-empty",
		StartNodeID: "review",
		Nodes: []Node{
			{ID: "review", Next: []string{"approved", "rejected"}},
			{ID: "approved"},
			{ID: "rejected"},
		},
	}))
	require.NoError(t, engine.CreateInstance(ctx, ID("wf-choice-empty"), "approval-choice-empty"))
	require.NoError(t, engine.StartInstance(ctx, ID("wf-choice-empty")))

	err := engine.AdvanceNodeTo(ctx, ID("wf-choice-empty"), "review", " ")
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))

	state, err := store.Get(ctx, ID("wf-choice-empty"))
	require.NoError(t, err)
	require.Equal(t, []string{"review"}, state.ActiveNodeIDs)
	require.Empty(t, state.CompletedNodeIDs)
}

func TestEngine_AdvanceNodeToWaitsAtJoinWhenOtherIncomingBranchHasNotArrived(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := NewEngine(store)

	require.NoError(t, engine.SaveDefinition(ctx, &Definition{
		ID:          "approval-choice-merge",
		StartNodeID: "review",
		Nodes: []Node{
			{ID: "review", Next: []string{"approved", "manager_review"}},
			{ID: "manager_review", Next: []string{"approved"}},
			{ID: "approved"},
		},
	}))
	require.NoError(t, engine.CreateInstance(ctx, ID("wf-choice-merge"), "approval-choice-merge"))
	require.NoError(t, engine.StartInstance(ctx, ID("wf-choice-merge")))
	require.NoError(t, engine.AdvanceNodeTo(ctx, ID("wf-choice-merge"), "review", "approved"))

	state, err := store.Get(ctx, ID("wf-choice-merge"))
	require.NoError(t, err)
	require.Empty(t, state.ActiveNodeIDs)
	require.Len(t, state.PendingJoins, 1)
	require.Equal(t, "approved", state.PendingJoins[0].NodeID)
	require.Equal(t, []string{"review"}, state.PendingJoins[0].ArrivedFrom)
	require.Equal(t, "choice", state.History[len(state.History)-2].Action)
	require.Equal(t, "approved", state.History[len(state.History)-2].NodeID)
	require.Equal(t, "review", state.History[len(state.History)-2].FromNodeID)
	require.Equal(t, "join_wait", state.History[len(state.History)-1].Action)
}

func TestEngine_AdvanceNodeToActivatesExplicitTaskMergeWithoutWaitingSkippedBranch(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := NewEngine(store)

	require.NoError(t, engine.SaveDefinition(ctx, &Definition{
		ID:          "approval-choice-task-merge",
		StartNodeID: "review",
		Nodes: []Node{
			{ID: "review", Kind: NodeKindBranch, Next: []string{"approved", "manager_review"}},
			{ID: "manager_review", Kind: NodeKindTask, Next: []string{"approved"}},
			{ID: "approved", Kind: NodeKindTask},
		},
	}))
	require.NoError(t, engine.CreateInstance(ctx, ID("wf-choice-task-merge"), "approval-choice-task-merge"))
	require.NoError(t, engine.StartInstance(ctx, ID("wf-choice-task-merge")))
	require.NoError(t, engine.AdvanceNodeTo(ctx, ID("wf-choice-task-merge"), "review", "approved"))

	state, err := store.Get(ctx, ID("wf-choice-task-merge"))
	require.NoError(t, err)
	require.Equal(t, []string{"approved"}, state.ActiveNodeIDs)
	require.Empty(t, state.PendingJoins)
	require.Equal(t, []string{"review"}, state.CompletedNodeIDs)
}

func TestEngine_AdvanceNodeToPreservesJoinWaitForSingleSuccessor(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := NewEngine(store)

	require.NoError(t, engine.SaveDefinition(ctx, &Definition{
		ID:          "approval-choice-single-join",
		StartNodeID: "start",
		Nodes: []Node{
			{ID: "start", Next: []string{"parallel", "choice"}},
			{ID: "parallel", Next: []string{"join"}},
			{ID: "choice", Next: []string{"join"}},
			{ID: "join", Next: []string{"archive"}},
			{ID: "archive"},
		},
	}))

	require.NoError(t, engine.CreateInstance(ctx, ID("wf-choice-single-join-exclusive"), "approval-choice-single-join"))
	require.NoError(t, engine.StartInstance(ctx, ID("wf-choice-single-join-exclusive")))
	require.NoError(t, engine.AdvanceNode(ctx, ID("wf-choice-single-join-exclusive"), "start"))
	require.NoError(t, engine.AdvanceNodeTo(ctx, ID("wf-choice-single-join-exclusive"), "choice", "join"))

	state, err := store.Get(ctx, ID("wf-choice-single-join-exclusive"))
	require.NoError(t, err)
	require.Equal(t, []string{"parallel"}, state.ActiveNodeIDs)
	require.Len(t, state.PendingJoins, 1)
	require.Equal(t, "join", state.PendingJoins[0].NodeID)
	require.Equal(t, []string{"choice"}, state.PendingJoins[0].ArrivedFrom)

	require.NoError(t, engine.CreateInstance(ctx, ID("wf-choice-single-join-default"), "approval-choice-single-join"))
	require.NoError(t, engine.StartInstance(ctx, ID("wf-choice-single-join-default")))
	require.NoError(t, engine.AdvanceNode(ctx, ID("wf-choice-single-join-default"), "start"))
	require.NoError(t, engine.AdvanceNode(ctx, ID("wf-choice-single-join-default"), "choice"))

	state, err = store.Get(ctx, ID("wf-choice-single-join-default"))
	require.NoError(t, err)
	require.Equal(t, []string{"parallel"}, state.ActiveNodeIDs)
	require.Len(t, state.PendingJoins, 1)
	require.Equal(t, "join", state.PendingJoins[0].NodeID)
}

func TestEngine_AdvanceNodeToKeepsExistingJoinWaitUntilAllBranchesArrive(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := NewEngine(store)

	require.NoError(t, engine.SaveDefinition(ctx, &Definition{
		ID:          "approval-choice-stale-join",
		StartNodeID: "start",
		Nodes: []Node{
			{ID: "start", Next: []string{"parallel", "choice"}},
			{ID: "parallel", Next: []string{"join"}},
			{ID: "choice", Next: []string{"join", "skip"}},
			{ID: "join", Next: []string{"archive"}},
			{ID: "skip"},
			{ID: "archive"},
		},
	}))
	require.NoError(t, engine.CreateInstance(ctx, ID("wf-choice-stale-join"), "approval-choice-stale-join"))
	require.NoError(t, engine.StartInstance(ctx, ID("wf-choice-stale-join")))
	require.NoError(t, engine.AdvanceNode(ctx, ID("wf-choice-stale-join"), "start"))
	require.NoError(t, engine.AdvanceNode(ctx, ID("wf-choice-stale-join"), "parallel"))

	state, err := store.Get(ctx, ID("wf-choice-stale-join"))
	require.NoError(t, err)
	require.Equal(t, []string{"choice"}, state.ActiveNodeIDs)
	require.Len(t, state.PendingJoins, 1)
	require.Equal(t, "join", state.PendingJoins[0].NodeID)

	require.NoError(t, engine.AdvanceNodeTo(ctx, ID("wf-choice-stale-join"), "choice", "join"))

	state, err = store.Get(ctx, ID("wf-choice-stale-join"))
	require.NoError(t, err)
	require.Equal(t, []string{"join"}, state.ActiveNodeIDs)
	require.Empty(t, state.PendingJoins)
	require.Equal(t, "choice", state.History[len(state.History)-2].Action)
	require.Equal(t, "join_ready", state.History[len(state.History)-1].Action)
	require.Equal(t, "join", state.CurrentNodeID)
}

func TestEngine_AdvanceNodeToCompletesJoinWhenRemainingBranchArrives(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := NewEngine(store)

	require.NoError(t, engine.SaveDefinition(ctx, &Definition{
		ID:          "approval-choice-active-join",
		StartNodeID: "start",
		Nodes: []Node{
			{ID: "start", Next: []string{"parallel", "choice"}},
			{ID: "parallel", Next: []string{"join"}},
			{ID: "choice", Next: []string{"join", "skip"}},
			{ID: "join", Next: []string{"archive"}},
			{ID: "skip"},
			{ID: "archive"},
		},
	}))
	require.NoError(t, engine.CreateInstance(ctx, ID("wf-choice-active-join"), "approval-choice-active-join"))
	require.NoError(t, engine.StartInstance(ctx, ID("wf-choice-active-join")))
	require.NoError(t, engine.AdvanceNode(ctx, ID("wf-choice-active-join"), "start"))
	require.NoError(t, engine.AdvanceNodeTo(ctx, ID("wf-choice-active-join"), "choice", "join"))

	state, err := store.Get(ctx, ID("wf-choice-active-join"))
	require.NoError(t, err)
	require.Equal(t, []string{"parallel"}, state.ActiveNodeIDs)
	require.Len(t, state.PendingJoins, 1)

	require.NoError(t, engine.AdvanceNode(ctx, ID("wf-choice-active-join"), "parallel"))

	state, err = store.Get(ctx, ID("wf-choice-active-join"))
	require.NoError(t, err)
	require.Equal(t, []string{"join"}, state.ActiveNodeIDs)
	require.Empty(t, state.PendingJoins)
}

func TestEngine_AdvanceNodeToRejectsUnreachableSuccessor(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := NewEngine(store)

	require.NoError(t, engine.SaveDefinition(ctx, &Definition{
		ID:          "approval-choice-invalid",
		StartNodeID: "review",
		Nodes: []Node{
			{ID: "review", Next: []string{"approved"}},
			{ID: "approved", Next: []string{"rejected"}},
			{ID: "rejected"},
		},
	}))
	require.NoError(t, engine.CreateInstance(ctx, ID("wf-choice-invalid"), "approval-choice-invalid"))
	require.NoError(t, engine.StartInstance(ctx, ID("wf-choice-invalid")))

	err := engine.AdvanceNodeTo(ctx, ID("wf-choice-invalid"), "review", "rejected")
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
}

func TestEngine_AdvanceNodeToDoesNotMutateStateOnInvalidSuccessor(t *testing.T) {
	engine := NewEngine(nil)
	def := &Definition{
		ID:          "approval-choice-invalid",
		StartNodeID: "review",
		Nodes: []Node{
			{ID: "review", Next: []string{"approved"}},
			{ID: "approved"},
		},
	}
	state := &State{
		ID:               "wf-choice-invalid",
		DefinitionID:     def.ID,
		Status:           InstanceStatusRunning,
		ActiveNodeIDs:    []string{"review"},
		CompletedNodeIDs: []string{},
		History:          []HistoryEntry{},
	}

	rejected := "rejected"
	err := engine.advanceNodeTo(state, def, "review", &rejected, time.Now())

	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
	require.Equal(t, []string{"review"}, state.ActiveNodeIDs)
	require.Empty(t, state.CompletedNodeIDs)
	require.Empty(t, state.History)
}

func TestEngine_AdvanceSerializesConcurrentLinearInstance(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := NewEngine(store)

	nodes := make([]Node, 64)
	wantCompleted := make([]string, len(nodes))
	for i := range nodes {
		id := "node-" + strconv.Itoa(i)
		nodes[i] = Node{ID: id}
		if i < len(nodes)-1 {
			nodes[i].Next = []string{"node-" + strconv.Itoa(i+1)}
		}
		wantCompleted[i] = id
	}

	require.NoError(t, engine.SaveDefinition(ctx, &Definition{
		ID:          "linear-concurrent",
		StartNodeID: nodes[0].ID,
		Nodes:       nodes,
	}))
	require.NoError(t, engine.CreateInstance(ctx, ID("wf-linear-concurrent"), "linear-concurrent"))
	require.NoError(t, engine.StartInstance(ctx, ID("wf-linear-concurrent")))

	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, len(nodes))
	for range nodes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- engine.Advance(ctx, ID("wf-linear-concurrent"))
		}()
	}

	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	state, err := store.Get(ctx, ID("wf-linear-concurrent"))
	require.NoError(t, err)
	require.Equal(t, InstanceStatusCompleted, state.Status)
	require.Equal(t, wantCompleted, state.CompletedNodeIDs)
}

func TestEngine_AdvanceNodeSerializesConcurrentJoinArrivals(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := NewEngine(store)

	require.NoError(t, engine.SaveDefinition(ctx, &Definition{
		ID:          "join-concurrent",
		StartNodeID: "start",
		Nodes: []Node{
			{ID: "start", Next: []string{"split"}},
			{ID: "split", Next: []string{"left", "right"}},
			{ID: "left", Next: []string{"join"}},
			{ID: "right", Next: []string{"join"}},
			{ID: "join", Next: []string{"archive"}},
			{ID: "archive"},
		},
	}))
	require.NoError(t, engine.CreateInstance(ctx, ID("wf-join-concurrent"), "join-concurrent"))
	require.NoError(t, engine.StartInstance(ctx, ID("wf-join-concurrent")))
	require.NoError(t, engine.AdvanceNode(ctx, ID("wf-join-concurrent"), "start"))
	require.NoError(t, engine.AdvanceNode(ctx, ID("wf-join-concurrent"), "split"))

	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, nodeID := range []string{"left", "right"} {
		wg.Add(1)
		go func(nodeID string) {
			defer wg.Done()
			<-start
			errs <- engine.AdvanceNode(ctx, ID("wf-join-concurrent"), nodeID)
		}(nodeID)
	}

	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	state, err := store.Get(ctx, ID("wf-join-concurrent"))
	require.NoError(t, err)
	require.Equal(t, []string{"join"}, state.ActiveNodeIDs)
	require.Empty(t, state.PendingJoins)
	require.ElementsMatch(t, []string{"start", "split", "left", "right"}, state.CompletedNodeIDs)
	require.Equal(t, "join_ready", state.History[len(state.History)-1].Action)
}

func TestEngine_StartInstanceSerializesConcurrentCalls(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := NewEngine(store)

	require.NoError(t, engine.SaveDefinition(ctx, &Definition{
		ID:          "start-concurrent",
		StartNodeID: "only",
		Nodes:       []Node{{ID: "only"}},
	}))
	require.NoError(t, engine.CreateInstance(ctx, ID("wf-start-concurrent"), "start-concurrent"))

	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < cap(errs); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- engine.StartInstance(ctx, ID("wf-start-concurrent"))
		}()
	}

	close(start)
	wg.Wait()
	close(errs)

	successes := 0
	conflicts := 0
	for err := range errs {
		if err == nil {
			successes++
			continue
		}
		require.True(t, errors.Is(err, errors.Conflict))
		conflicts++
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 7, conflicts)
}

func TestEngine_StartInstanceAcrossEnginesUsesStoreVersioning(t *testing.T) {
	ctx := context.Background()
	store := newBlockingVersionStore()
	engineA := NewEngine(store)
	engineB := NewEngine(store)

	require.NoError(t, engineA.SaveDefinition(ctx, &Definition{
		ID:          "cross-engine",
		StartNodeID: "only",
		Nodes:       []Node{{ID: "only"}},
	}))
	require.NoError(t, engineA.CreateInstance(ctx, ID("wf-cross-engine"), "cross-engine"))

	started := make(chan struct{}, 2)
	done := make(chan error, 2)
	go func() {
		started <- struct{}{}
		done <- engineA.StartInstance(ctx, ID("wf-cross-engine"))
	}()
	go func() {
		started <- struct{}{}
		done <- engineB.StartInstance(ctx, ID("wf-cross-engine"))
	}()

	<-started
	<-started
	<-store.ready
	<-store.ready
	close(store.barrier)

	successes := 0
	conflicts := 0
	for i := 0; i < 2; i++ {
		err := <-done
		if err == nil {
			successes++
			continue
		}
		require.True(t, errors.Is(err, errors.Conflict), "expected conflict, got %v", err)
		conflicts++
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, conflicts)

	state, err := store.Get(ctx, ID("wf-cross-engine"))
	require.NoError(t, err)
	require.Equal(t, InstanceStatusRunning, state.Status)
	require.Equal(t, uint64(2), state.Version)
}

func TestEngine_AdvanceNodeRejectsInactiveNode(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := NewEngine(store)

	require.NoError(t, engine.SaveDefinition(ctx, &Definition{
		ID:          "branch-invalid",
		StartNodeID: "start",
		Nodes: []Node{
			{ID: "start", Next: []string{"split"}},
			{ID: "split", Next: []string{"left", "right"}},
			{ID: "left"},
			{ID: "right"},
		},
	}))
	require.NoError(t, engine.CreateInstance(ctx, ID("wf-inactive"), "branch-invalid"))
	require.NoError(t, engine.StartInstance(ctx, ID("wf-inactive")))

	err := engine.AdvanceNode(ctx, ID("wf-inactive"), "left")
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Conflict))
}

func TestEngine_AdvanceRejectsAmbiguousBranchState(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := NewEngine(store)

	require.NoError(t, engine.SaveDefinition(ctx, &Definition{
		ID:          "branch-advance",
		StartNodeID: "start",
		Nodes: []Node{
			{ID: "start", Next: []string{"left", "right"}},
			{ID: "left"},
			{ID: "right"},
		},
	}))
	require.NoError(t, engine.CreateInstance(ctx, ID("wf-advance"), "branch-advance"))
	require.NoError(t, engine.StartInstance(ctx, ID("wf-advance")))
	require.NoError(t, engine.AdvanceNode(ctx, ID("wf-advance"), "start"))

	err := engine.Advance(ctx, ID("wf-advance"))
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Conflict))
}

func TestEngine_SaveDefinitionRejectsInvalidGraph(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := NewEngine(store)

	err := engine.SaveDefinition(ctx, &Definition{
		ID:          "missing-next",
		StartNodeID: "start",
		Nodes: []Node{
			{ID: "start", Next: []string{"missing"}},
		},
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))

	err = engine.SaveDefinition(ctx, &Definition{
		ID:          "cycle",
		StartNodeID: "a",
		Nodes: []Node{
			{ID: "a", Next: []string{"b"}},
			{ID: "b", Next: []string{"a"}},
		},
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))

	err = engine.SaveDefinition(ctx, &Definition{
		ID:          "unreachable",
		StartNodeID: "a",
		Nodes: []Node{
			{ID: "a"},
			{ID: "b"},
		},
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))

	err = engine.SaveDefinition(ctx, &Definition{
		ID:          "start-incoming",
		StartNodeID: "start",
		Nodes: []Node{
			{ID: "start", Next: []string{"next"}},
			{ID: "next", Next: []string{"start"}},
		},
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
}

func TestEngine_AdvanceRejectsCorruptedActiveNode(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := NewEngine(store)

	require.NoError(t, engine.SaveDefinition(ctx, &Definition{
		ID:          "linear",
		StartNodeID: "step-a",
		Nodes: []Node{
			{ID: "step-a", Next: []string{"step-b"}},
			{ID: "step-b"},
		},
	}))
	require.NoError(t, engine.CreateInstance(ctx, ID("wf-2"), "linear"))
	require.NoError(t, engine.StartInstance(ctx, ID("wf-2")))

	state, err := store.Get(ctx, ID("wf-2"))
	require.NoError(t, err)
	state.ActiveNodeIDs = []string{"missing"}
	require.NoError(t, store.Save(ctx, state))

	err = engine.Advance(ctx, ID("wf-2"))
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
}

func TestEngine_CreateInstanceRejectsDuplicateAndInvalidDefinition(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := NewEngine(store)

	err := engine.SaveDefinition(ctx, &Definition{
		ID:          "invalid",
		StartNodeID: "dup",
		Nodes: []Node{
			{ID: "dup"},
			{ID: "dup"},
		},
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))

	require.NoError(t, engine.SaveDefinition(ctx, &Definition{
		ID:          "valid",
		StartNodeID: "only-node",
		Nodes:       []Node{{ID: "only-node"}},
	}))
	require.NoError(t, engine.CreateInstance(ctx, ID("wf-3"), "valid"))

	err = engine.CreateInstance(ctx, ID("wf-3"), "valid")
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Conflict))

	err = engine.CreateInstance(ctx, ID("wf-4"), "missing")
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.NotFound))
}

func TestEngine_CreateInstancePersistsInitialDataAtomically(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := NewEngine(store)
	initial := map[string]any{
		"request_id": "REQ-1",
		"items":      []map[string]any{{"sku": "SKU-1"}},
	}

	require.NoError(t, engine.SaveDefinition(ctx, &Definition{
		ID:          "with-data",
		StartNodeID: "node",
		Nodes:       []Node{{ID: "node"}},
	}))
	require.NoError(t, engine.CreateInstanceWithData(ctx, ID("wf-with-data"), "with-data", initial))
	initial["request_id"] = "mutated"
	initial["items"].([]map[string]any)[0]["sku"] = "mutated"

	state, err := store.Get(ctx, ID("wf-with-data"))
	require.NoError(t, err)
	require.Equal(t, uint64(1), state.Version)
	require.Equal(t, "REQ-1", state.Data["request_id"])
	require.Equal(t, "SKU-1", state.Data["items"].([]map[string]any)[0]["sku"])
}

func TestEngine_PublicMethodsRejectNilContextBeforeStore(t *testing.T) {
	store := &nilContextStore{}
	engine := NewEngine(store)
	definition := &Definition{
		ID:          "nil-context",
		StartNodeID: "node",
		Nodes:       []Node{{ID: "node"}},
	}

	err := engine.SaveDefinition(nil, definition)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))

	err = engine.CreateInstance(nil, ID("wf-nil"), definition.ID)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))

	err = engine.StartInstance(nil, ID("wf-nil"))
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))

	err = engine.Advance(nil, ID("wf-nil"))
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))

	err = engine.AdvanceNode(nil, ID("wf-nil"), "node")
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))

	require.False(t, store.called)
}

func TestEngine_CreateInstanceSerializesConcurrentDuplicate(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := NewEngine(store)

	require.NoError(t, engine.SaveDefinition(ctx, &Definition{
		ID:          "create-concurrent",
		StartNodeID: "node",
		Nodes:       []Node{{ID: "node"}},
	}))

	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < cap(errs); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- engine.CreateInstance(ctx, ID("wf-create-concurrent"), "create-concurrent")
		}()
	}

	close(start)
	wg.Wait()
	close(errs)

	successes := 0
	conflicts := 0
	for err := range errs {
		if err == nil {
			successes++
			continue
		}
		require.True(t, errors.Is(err, errors.Conflict))
		conflicts++
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, conflicts)
}

type nilContextStore struct {
	called bool
}

func (s *nilContextStore) markCalled() error {
	s.called = true
	return errors.New("store should not be called")
}

func (s *nilContextStore) GetDefinition(ctx context.Context, id string) (*Definition, error) {
	return nil, s.markCalled()
}

func (s *nilContextStore) SaveDefinition(ctx context.Context, def *Definition) error {
	return s.markCalled()
}

func (s *nilContextStore) DeleteDefinition(ctx context.Context, id string) error {
	return s.markCalled()
}

func (s *nilContextStore) Get(ctx context.Context, id ID) (*State, error) {
	return nil, s.markCalled()
}

func (s *nilContextStore) Save(ctx context.Context, st *State) error {
	return s.markCalled()
}

func (s *nilContextStore) Delete(ctx context.Context, id ID) error {
	return s.markCalled()
}

type failingStore struct{}

func (failingStore) GetDefinition(ctx context.Context, id string) (*Definition, error) {
	return nil, errors.New("store get definition failed")
}

func (failingStore) SaveDefinition(ctx context.Context, def *Definition) error {
	return errors.New("store save definition failed")
}

func (failingStore) DeleteDefinition(ctx context.Context, id string) error {
	return nil
}

func (failingStore) Get(ctx context.Context, id ID) (*State, error) {
	return nil, errors.New("store get failed")
}

func (failingStore) Save(ctx context.Context, st *State) error {
	return errors.New("store save failed")
}

func (failingStore) Delete(ctx context.Context, id ID) error { return nil }

type blockingVersionStore struct {
	*MemoryStore
	ready   chan struct{}
	barrier chan struct{}
}

func newBlockingVersionStore() *blockingVersionStore {
	return &blockingVersionStore{
		MemoryStore: NewMemoryStore(),
		ready:       make(chan struct{}, 2),
		barrier:     make(chan struct{}),
	}
}

func (s *blockingVersionStore) SaveIfVersion(ctx context.Context, st *State, expectedVersion uint64) error {
	if expectedVersion > 0 {
		s.ready <- struct{}{}
		<-s.barrier
	}
	return s.MemoryStore.SaveIfVersion(ctx, st, expectedVersion)
}

func TestEngine_updateInstanceData_ReturnsGetError(t *testing.T) {
	ctx := context.Background()
	engine := NewEngine(failingStore{})

	err := engine.updateInstanceData(ctx, ID("wf-4"), func(data map[string]any) error { return nil })
	require.Error(t, err)
}

func TestEngine_updateInstanceData_RejectsNilUpdate(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := NewEngine(store)

	err := engine.updateInstanceData(ctx, ID("wf-5"), nil)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
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
