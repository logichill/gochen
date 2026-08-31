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

func TestEngine_SaveDefinitionPreservesCreatedAtOnUpdate(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	manualClock := clock.NewManualClock(time.Date(2026, 5, 24, 10, 0, 0, 0, time.UTC))
	engine := NewEngine(store).WithClock(manualClock)

	require.NoError(t, engine.SaveDefinition(ctx, &Definition{
		ID:          "definition-timestamps",
		StartNodeID: "start",
		Nodes:       []Node{{ID: "start"}},
	}))
	created, err := store.GetDefinition(ctx, "definition-timestamps", 0)
	require.NoError(t, err)
	require.Equal(t, manualClock.Now(), created.CreatedAt)
	require.Equal(t, manualClock.Now(), created.UpdatedAt)

	manualClock.Advance(time.Hour)
	require.NoError(t, engine.SaveDefinition(ctx, &Definition{
		ID:          "definition-timestamps",
		Name:        "updated",
		StartNodeID: "start",
		Nodes:       []Node{{ID: "start", Name: "renamed"}},
	}))

	updated, err := store.GetDefinition(ctx, "definition-timestamps", 0)
	require.NoError(t, err)
	require.Equal(t, created.CreatedAt, updated.CreatedAt)
	require.Equal(t, manualClock.Now(), updated.UpdatedAt)
	require.True(t, updated.UpdatedAt.After(updated.CreatedAt))
	require.Equal(t, "updated", updated.Name)
	require.Equal(t, "renamed", updated.Nodes[0].Name)
}

func TestEngine_SaveDefinitionSkipsLookupWhenCreatedAtProvided(t *testing.T) {
	ctx := context.Background()
	store := &saveOnlyDefinitionStore{}
	manualClock := clock.NewManualClock(time.Date(2026, 5, 24, 11, 0, 0, 0, time.UTC))
	engine := NewEngine(store).WithClock(manualClock)
	createdAt := time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC)

	require.NoError(t, engine.SaveDefinition(ctx, &Definition{
		ID:          "definition-with-created-at",
		Version:     1,
		StartNodeID: "start",
		Nodes:       []Node{{ID: "start"}},
		CreatedAt:   createdAt,
	}))

	require.Zero(t, store.getDefinitionCalls)
	require.NotNil(t, store.saved)
	require.Equal(t, createdAt, store.saved.CreatedAt)
	require.Equal(t, manualClock.Now(), store.saved.UpdatedAt)
}

func TestEngine_SaveDefinitionSerializesConcurrentCreateForSameID(t *testing.T) {
	ctx := context.Background()
	store := newBlockingMissingDefinitionStore()
	manualClock := clock.NewManualClock(time.Date(2026, 5, 26, 9, 0, 0, 0, time.UTC))
	engine := NewEngine(store).WithClock(manualClock)

	errs := make(chan error, 2)
	go func() {
		errs <- engine.SaveDefinition(ctx, &Definition{
			ID:          "definition-concurrent-create",
			Name:        "first",
			StartNodeID: "start",
			Nodes:       []Node{{ID: "start"}},
		})
	}()

	<-store.firstMissingGetStarted

	go func() {
		errs <- engine.SaveDefinition(ctx, &Definition{
			ID:          "definition-concurrent-create",
			Name:        "second",
			StartNodeID: "start",
			Nodes:       []Node{{ID: "start", Name: "renamed"}},
		})
	}()

	select {
	case <-store.secondMissingGetStarted:
		t.Fatal("expected same definition saves to serialize before second GetDefinition")
	case <-time.After(50 * time.Millisecond):
	}

	manualClock.Advance(time.Hour)
	close(store.releaseFirstMissingGet)

	for i := 0; i < 2; i++ {
		require.NoError(t, <-errs)
	}

	saved, err := store.GetDefinition(ctx, "definition-concurrent-create", 0)
	require.NoError(t, err)
	require.NotNil(t, saved)
	require.Equal(t, time.Date(2026, 5, 26, 9, 0, 0, 0, time.UTC), saved.CreatedAt)
	require.Equal(t, manualClock.Now(), saved.UpdatedAt)
	require.Equal(t, "second", saved.Name)
	require.Equal(t, "renamed", saved.Nodes[0].Name)
}

func TestEngine_SaveDefinitionAllowsDifferentIDsWhileOneLookupBlocks(t *testing.T) {
	ctx := context.Background()
	store := newBlockingMissingDefinitionStore()
	engine := NewEngine(store)

	firstErr := make(chan error, 1)
	go func() {
		firstErr <- engine.SaveDefinition(ctx, &Definition{
			ID:          "blocked-definition",
			StartNodeID: "start",
			Nodes:       []Node{{ID: "start"}},
		})
	}()

	<-store.firstMissingGetStarted

	secondErr := make(chan error, 1)
	go func() {
		secondErr <- engine.SaveDefinition(ctx, &Definition{
			ID:          "independent-definition",
			StartNodeID: "start",
			Nodes:       []Node{{ID: "start"}},
		})
	}()

	select {
	case err := <-secondErr:
		require.NoError(t, err)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected different definition saves not to wait on blocked lookup")
	}

	close(store.releaseFirstMissingGet)
	require.NoError(t, <-firstErr)
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

func TestEngine_DirectStructUsesDefaultClockForConcurrentAdvancePaths(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := &Engine{store: store}

	require.NoError(t, engine.SaveDefinition(ctx, &Definition{
		ID:          "concurrent-advance-default-clock",
		StartNodeID: "start",
		Nodes: []Node{
			{ID: "start", Edges: NewEdges("done")},
			{ID: "done"},
		},
	}))
	require.NoError(t, engine.SaveDefinition(ctx, &Definition{
		ID:          "concurrent-advance-node-default-clock",
		StartNodeID: "review",
		Nodes: []Node{
			{ID: "review", Edges: NewEdges("approved", "rejected")},
			{ID: "approved"},
			{ID: "rejected"},
		},
	}))
	require.NoError(t, engine.CreateInstance(ctx, ID("wf-advance"), "concurrent-advance-default-clock"))
	require.NoError(t, engine.CreateInstance(ctx, ID("wf-advance-node"), "concurrent-advance-node-default-clock"))
	require.NoError(t, engine.StartInstance(ctx, ID("wf-advance")))
	require.NoError(t, engine.StartInstance(ctx, ID("wf-advance-node")))

	start := make(chan struct{})
	errs := make(chan error, 2)
	go func() {
		<-start
		errs <- engine.Advance(ctx, ID("wf-advance"))
	}()
	go func() {
		<-start
		errs <- engine.AdvanceNode(ctx, ID("wf-advance-node"), "review")
	}()
	close(start)

	for i := 0; i < 2; i++ {
		require.NoError(t, <-errs)
	}
}

func TestEngine_AdvanceNodeToLeavesStateUnchangedOnActivationError(t *testing.T) {
	engine := NewEngine(NewMemoryStore())
	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	definition := &Definition{
		ID:          "duplicate-join-arrival",
		StartNodeID: "a",
		Nodes: []Node{
			{ID: "a", Edges: NewEdges("join")},
			{ID: "b", Edges: NewEdges("join")},
			{ID: "join", Kind: NodeKindJoin},
		},
	}
	state := &State{
		ID:               ID("wf-duplicate-join-arrival"),
		DefinitionID:     definition.ID,
		Status:           InstanceStatusRunning,
		ActiveNodeIDs:    []string{"a"},
		CompletedNodeIDs: []string{"b"},
		PendingJoins: []PendingJoin{{
			NodeID:        "join",
			ExpectedCount: 2,
			ArrivedFrom:   []string{"a"},
		}},
		History: []HistoryEntry{{Action: "start", NodeID: "a", At: now.Add(-time.Minute)}},
		Data:    map[string]any{"request_id": "REQ-1"},
	}
	original := cloneState(state)

	err := engine.advanceNodeTo(context.Background(), state, definition, "a", nil, now)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Conflict))
	require.Equal(t, original, state)
}

func TestEngine_BranchJoinLifecycle(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := NewEngine(store)

	definition := &Definition{
		ID:          "branching",
		StartNodeID: "prepare",
		Nodes: []Node{
			{ID: "prepare", Edges: NewEdges("split")},
			{ID: "split", Edges: NewEdges("email", "invoice")},
			{ID: "email", Edges: NewEdges("join")},
			{ID: "invoice", Edges: NewEdges("join")},
			{ID: "join", Kind: NodeKindJoin, Edges: NewEdges("archive")},
			{ID: "archive"},
		},
	}

	require.NoError(t, engine.SaveDefinition(ctx, definition))
	require.NoError(t, engine.CreateInstance(ctx, ID("wf-branch"), definition.ID))
	require.NoError(t, engine.StartInstance(ctx, ID("wf-branch")))

	state, err := store.Get(ctx, ID("wf-branch"))
	require.NoError(t, err)
	require.Equal(t, []string{"prepare"}, state.ActiveNodeIDs)

	require.NoError(t, engine.AdvanceNode(ctx, ID("wf-branch"), "prepare"))

	state, err = store.Get(ctx, ID("wf-branch"))
	require.NoError(t, err)
	require.Equal(t, []string{"split"}, state.ActiveNodeIDs)
	require.Equal(t, []string{"prepare"}, state.CompletedNodeIDs)

	require.NoError(t, engine.AdvanceNode(ctx, ID("wf-branch"), "split"))

	state, err = store.Get(ctx, ID("wf-branch"))
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"email", "invoice"}, state.ActiveNodeIDs)
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
			{ID: "review", Edges: NewEdges("approved", "rejected")},
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
			{ID: "review", Edges: NewEdges("approved", "rejected")},
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
			{ID: "review", Edges: NewEdges("approved", "manager_review")},
			{ID: "manager_review", Edges: NewEdges("approved")},
			{ID: "approved", Kind: NodeKindJoin},
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
			{ID: "review", Kind: NodeKindBranch, Edges: NewEdges("approved", "manager_review")},
			{ID: "manager_review", Kind: NodeKindTask, Edges: NewEdges("approved")},
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
			{ID: "start", Edges: NewEdges("parallel", "choice")},
			{ID: "parallel", Edges: NewEdges("join")},
			{ID: "choice", Edges: NewEdges("join")},
			{ID: "join", Kind: NodeKindJoin, Edges: NewEdges("archive")},
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
			{ID: "start", Edges: NewEdges("parallel", "choice")},
			{ID: "parallel", Edges: NewEdges("join")},
			{ID: "choice", Edges: NewEdges("join", "skip")},
			{ID: "join", Kind: NodeKindJoin, Edges: NewEdges("archive")},
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
}

func TestEngine_AdvanceNodeToCompletesJoinWhenRemainingBranchArrives(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := NewEngine(store)

	require.NoError(t, engine.SaveDefinition(ctx, &Definition{
		ID:          "approval-choice-active-join",
		StartNodeID: "start",
		Nodes: []Node{
			{ID: "start", Edges: NewEdges("parallel", "choice")},
			{ID: "parallel", Edges: NewEdges("join")},
			{ID: "choice", Edges: NewEdges("join", "skip")},
			{ID: "join", Kind: NodeKindJoin, Edges: NewEdges("archive")},
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
			{ID: "review", Edges: NewEdges("approved")},
			{ID: "approved", Edges: NewEdges("rejected")},
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
			{ID: "review", Edges: NewEdges("approved")},
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
	err := engine.advanceNodeTo(context.Background(), state, def, "review", &rejected, time.Now())

	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
	require.Equal(t, []string{"review"}, state.ActiveNodeIDs)
	require.Empty(t, state.CompletedNodeIDs)
	require.Empty(t, state.History)
}

func TestEngine_RejectNodeReactivatesCompletedAncestor(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := NewEngine(store)

	require.NoError(t, engine.SaveDefinition(ctx, &Definition{
		ID:          "approval-reject-linear",
		StartNodeID: "submit",
		Nodes: []Node{
			{ID: "submit", Edges: NewEdges("review")},
			{ID: "review", Edges: NewEdges("approve")},
			{ID: "approve", RejectTo: []string{"review"}},
		},
	}))
	require.NoError(t, engine.CreateInstance(ctx, ID("wf-reject-linear"), "approval-reject-linear"))
	require.NoError(t, engine.StartInstance(ctx, ID("wf-reject-linear")))
	require.NoError(t, engine.AdvanceNode(ctx, ID("wf-reject-linear"), "submit"))
	require.NoError(t, engine.AdvanceNode(ctx, ID("wf-reject-linear"), "review"))

	require.NoError(t, engine.RejectNode(ctx, ID("wf-reject-linear"), "approve", "review"))
	state, err := store.Get(ctx, ID("wf-reject-linear"))
	require.NoError(t, err)
	require.Equal(t, InstanceStatusRunning, state.Status)
	require.Equal(t, []string{"review"}, state.ActiveNodeIDs)
	require.Equal(t, []string{"submit"}, state.CompletedNodeIDs)
	require.Equal(t, "reject", state.History[len(state.History)-1].Action)

	require.NoError(t, engine.AdvanceNode(ctx, ID("wf-reject-linear"), "review"))
	state, err = store.Get(ctx, ID("wf-reject-linear"))
	require.NoError(t, err)
	require.Equal(t, []string{"approve"}, state.ActiveNodeIDs)
	require.Equal(t, []string{"submit", "review"}, state.CompletedNodeIDs)
}

func TestEngine_RejectNodeCanReturnToStartNode(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := NewEngine(store)

	require.NoError(t, engine.SaveDefinition(ctx, &Definition{
		ID:          "approval-reject-start",
		StartNodeID: "submit",
		Nodes: []Node{
			{ID: "submit", Edges: NewEdges("review")},
			{ID: "review", Edges: NewEdges("approve")},
			{ID: "approve", RejectTo: []string{"submit"}},
		},
	}))
	require.NoError(t, engine.CreateInstance(ctx, ID("wf-reject-start"), "approval-reject-start"))
	require.NoError(t, engine.StartInstance(ctx, ID("wf-reject-start")))
	require.NoError(t, engine.AdvanceNode(ctx, ID("wf-reject-start"), "submit"))
	require.NoError(t, engine.AdvanceNode(ctx, ID("wf-reject-start"), "review"))

	require.NoError(t, engine.RejectNode(ctx, ID("wf-reject-start"), "approve", "submit"))
	state, err := store.Get(ctx, ID("wf-reject-start"))
	require.NoError(t, err)
	require.Equal(t, []string{"submit"}, state.ActiveNodeIDs)
	require.Empty(t, state.CompletedNodeIDs)
}

func TestEngine_RejectNodePreservesUnaffectedJoinArrivals(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := NewEngine(store)

	require.NoError(t, engine.SaveDefinition(ctx, &Definition{
		ID:          "approval-reject-branch",
		StartNodeID: "start",
		Nodes: []Node{
			{ID: "start", Edges: NewEdges("split")},
			{ID: "split", Edges: NewEdges("left_prepare", "right_prepare")},
			{ID: "left_prepare", Edges: NewEdges("left_review")},
			{ID: "left_review", Edges: NewEdges("join"), RejectTo: []string{"left_prepare"}},
			{ID: "right_prepare", Edges: NewEdges("join")},
			{ID: "join", Kind: NodeKindJoin, Edges: NewEdges("archive")},
			{ID: "archive"},
		},
	}))
	require.NoError(t, engine.CreateInstance(ctx, ID("wf-reject-branch"), "approval-reject-branch"))
	require.NoError(t, engine.StartInstance(ctx, ID("wf-reject-branch")))
	require.NoError(t, engine.AdvanceNode(ctx, ID("wf-reject-branch"), "start"))
	require.NoError(t, engine.AdvanceNode(ctx, ID("wf-reject-branch"), "split"))
	require.NoError(t, engine.AdvanceNode(ctx, ID("wf-reject-branch"), "right_prepare"))
	require.NoError(t, engine.AdvanceNode(ctx, ID("wf-reject-branch"), "left_prepare"))

	state, err := store.Get(ctx, ID("wf-reject-branch"))
	require.NoError(t, err)
	require.Equal(t, []string{"left_review"}, state.ActiveNodeIDs)
	require.Len(t, state.PendingJoins, 1)
	require.Equal(t, []string{"right_prepare"}, state.PendingJoins[0].ArrivedFrom)

	require.NoError(t, engine.RejectNode(ctx, ID("wf-reject-branch"), "left_review", "left_prepare"))
	state, err = store.Get(ctx, ID("wf-reject-branch"))
	require.NoError(t, err)
	require.Equal(t, []string{"left_prepare"}, state.ActiveNodeIDs)
	require.Len(t, state.PendingJoins, 1)
	require.Equal(t, []string{"right_prepare"}, state.PendingJoins[0].ArrivedFrom)
	require.ElementsMatch(t, []string{"start", "split", "right_prepare"}, state.CompletedNodeIDs)

	require.NoError(t, engine.AdvanceNode(ctx, ID("wf-reject-branch"), "left_prepare"))
	require.NoError(t, engine.AdvanceNode(ctx, ID("wf-reject-branch"), "left_review"))
	state, err = store.Get(ctx, ID("wf-reject-branch"))
	require.NoError(t, err)
	require.Equal(t, []string{"join"}, state.ActiveNodeIDs)
	require.Empty(t, state.PendingJoins)
}

func TestEngine_RejectNodeRejectsInactiveNodeAndDisallowedTarget(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := NewEngine(store)

	require.NoError(t, engine.SaveDefinition(ctx, &Definition{
		ID:          "approval-reject-invalid",
		StartNodeID: "submit",
		Nodes: []Node{
			{ID: "submit", Edges: NewEdges("review")},
			{ID: "review", Edges: NewEdges("approve")},
			{ID: "approve", RejectTo: []string{"review"}},
		},
	}))
	require.NoError(t, engine.CreateInstance(ctx, ID("wf-reject-invalid"), "approval-reject-invalid"))
	require.NoError(t, engine.StartInstance(ctx, ID("wf-reject-invalid")))

	err := engine.RejectNode(ctx, ID("wf-reject-invalid"), "approve", "review")
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Conflict))

	require.NoError(t, engine.AdvanceNode(ctx, ID("wf-reject-invalid"), "submit"))
	require.NoError(t, engine.AdvanceNode(ctx, ID("wf-reject-invalid"), "review"))
	err = engine.RejectNode(ctx, ID("wf-reject-invalid"), "approve", "submit")
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
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
			nodes[i].Edges = NewEdges("node-" + strconv.Itoa(i+1))
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
			{ID: "start", Edges: NewEdges("split")},
			{ID: "split", Edges: NewEdges("left", "right")},
			{ID: "left", Edges: NewEdges("join")},
			{ID: "right", Edges: NewEdges("join")},
			{ID: "join", Kind: NodeKindJoin, Edges: NewEdges("archive")},
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
		require.Truef(t, errors.Is(err, errors.Conflict), "expected conflict, got %v", err)
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
			{ID: "start", Edges: NewEdges("split")},
			{ID: "split", Edges: NewEdges("left", "right")},
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
			{ID: "start", Edges: NewEdges("left", "right")},
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
			{ID: "start", Edges: NewEdges("missing")},
		},
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))

	err = engine.SaveDefinition(ctx, &Definition{
		ID:          "cycle",
		StartNodeID: "a",
		Nodes: []Node{
			{ID: "a", Edges: NewEdges("b")},
			{ID: "b", Edges: NewEdges("a")},
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
			{ID: "start", Edges: NewEdges("next")},
			{ID: "next", Edges: NewEdges("start")},
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
			{ID: "step-a", Edges: NewEdges("step-b")},
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

func (s *nilContextStore) GetDefinition(ctx context.Context, id string, version uint32) (*Definition, error) {
	return nil, s.markCalled()
}

func (s *nilContextStore) SaveDefinition(ctx context.Context, def *Definition) error {
	return s.markCalled()
}

func (s *nilContextStore) DeleteDefinition(ctx context.Context, id string, version uint32) error {
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

func (failingStore) GetDefinition(ctx context.Context, id string, version uint32) (*Definition, error) {
	return nil, errors.New("store get definition failed")
}

func (failingStore) SaveDefinition(ctx context.Context, def *Definition) error {
	return errors.New("store save definition failed")
}

func (failingStore) DeleteDefinition(ctx context.Context, id string, version uint32) error {
	return nil
}

func (failingStore) Get(ctx context.Context, id ID) (*State, error) {
	return nil, errors.New("store get failed")
}

func (failingStore) Save(ctx context.Context, st *State) error {
	return errors.New("store save failed")
}

func (failingStore) Delete(ctx context.Context, id ID) error { return nil }

type definitionSaveFailStore struct {
	*MemoryStore
}

func (s *definitionSaveFailStore) SaveDefinition(ctx context.Context, def *Definition) error {
	return errors.New("store save definition failed")
}

type instanceSaveFailStore struct {
	*MemoryStore
}

func (s *instanceSaveFailStore) Save(ctx context.Context, st *State) error {
	return errors.New("store save failed")
}

func (s *instanceSaveFailStore) SaveIfVersion(ctx context.Context, st *State, expectedVersion uint64) error {
	return errors.New("store save failed")
}

type saveOnlyDefinitionStore struct {
	getDefinitionCalls int
	saved              *Definition
}

func (s *saveOnlyDefinitionStore) GetDefinition(ctx context.Context, id string, version uint32) (*Definition, error) {
	s.getDefinitionCalls++
	return nil, errors.New("store get definition should not be called")
}

func (s *saveOnlyDefinitionStore) SaveDefinition(ctx context.Context, def *Definition) error {
	s.saved = cloneDefinition(def)
	return nil
}

func (s *saveOnlyDefinitionStore) DeleteDefinition(ctx context.Context, id string, version uint32) error {
	return nil
}

func (s *saveOnlyDefinitionStore) Get(ctx context.Context, id ID) (*State, error) {
	return nil, nil
}

func (s *saveOnlyDefinitionStore) Save(ctx context.Context, st *State) error {
	return nil
}

func (s *saveOnlyDefinitionStore) Delete(ctx context.Context, id ID) error {
	return nil
}

type blockingMissingDefinitionStore struct {
	*MemoryStore
	firstMissingGetStarted  chan struct{}
	secondMissingGetStarted chan struct{}
	releaseFirstMissingGet  chan struct{}
	mu                      sync.Mutex
	firstSeen               bool
}

func newBlockingMissingDefinitionStore() *blockingMissingDefinitionStore {
	return &blockingMissingDefinitionStore{
		MemoryStore:             NewMemoryStore(),
		firstMissingGetStarted:  make(chan struct{}),
		secondMissingGetStarted: make(chan struct{}),
		releaseFirstMissingGet:  make(chan struct{}),
	}
}

func (s *blockingMissingDefinitionStore) GetDefinition(ctx context.Context, id string, version uint32) (*Definition, error) {
	def, err := s.MemoryStore.GetDefinition(ctx, id, version)
	if err != nil || def != nil {
		return def, err
	}

	s.mu.Lock()
	first := !s.firstSeen
	if first {
		s.firstSeen = true
	}
	s.mu.Unlock()

	if first {
		close(s.firstMissingGetStarted)
		<-s.releaseFirstMissingGet
		return nil, nil
	}

	close(s.secondMissingGetStarted)
	return nil, nil
}

type notFoundDefinitionStore struct {
	*MemoryStore
}

func (s notFoundDefinitionStore) GetDefinition(ctx context.Context, id string, version uint32) (*Definition, error) {
	def, err := s.MemoryStore.GetDefinition(ctx, id, version)
	if err != nil {
		return nil, err
	}
	if def == nil {
		return nil, errors.NewCode(errors.NotFound, "workflow definition not found")
	}
	return def, nil
}

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

func TestEngine_SaveDefinition_IgnoresNotFoundLookup(t *testing.T) {
	ctx := context.Background()
	store := notFoundDefinitionStore{MemoryStore: NewMemoryStore()}
	engine := NewEngine(store)

	err := engine.SaveDefinition(ctx, &Definition{
		ID:          "new-definition",
		StartNodeID: "start",
		Nodes:       []Node{{ID: "start"}},
	})
	require.NoError(t, err)

	saved, getErr := store.MemoryStore.GetDefinition(ctx, "new-definition", 0)
	require.NoError(t, getErr)
	require.NotNil(t, saved)
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

func TestEngine_StartInstanceWithMutationPersistsMutation(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := NewEngine(store)

	require.NoError(t, engine.SaveDefinition(ctx, &Definition{
		ID:          "start-with-mutation",
		StartNodeID: "start",
		Nodes:       []Node{{ID: "start"}},
	}))
	require.NoError(t, engine.CreateInstance(ctx, ID("wf-start-mutation"), "start-with-mutation"))

	require.NoError(t, engine.StartInstanceWithMutation(ctx, ID("wf-start-mutation"), func(st *State, now time.Time) error {
		st.Data["tag"] = "started"
		return nil
	}))

	state, err := store.Get(ctx, ID("wf-start-mutation"))
	require.NoError(t, err)
	require.Equal(t, "started", state.Data["tag"])
}

func TestEngine_AdvanceNodeWithMutationPersistsMutation(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := NewEngine(store)

	require.NoError(t, engine.SaveDefinition(ctx, &Definition{
		ID:          "advance-with-mutation",
		StartNodeID: "start",
		Nodes: []Node{
			{ID: "start", Edges: NewEdges("done")},
			{ID: "done"},
		},
	}))
	require.NoError(t, engine.CreateInstance(ctx, ID("wf-advance-mutation"), "advance-with-mutation"))
	require.NoError(t, engine.StartInstance(ctx, ID("wf-advance-mutation")))

	require.NoError(t, engine.AdvanceNodeWithMutation(ctx, ID("wf-advance-mutation"), "start", func(st *State, now time.Time) error {
		st.Data["tag"] = "advanced"
		return nil
	}))

	state, err := store.Get(ctx, ID("wf-advance-mutation"))
	require.NoError(t, err)
	require.Equal(t, "advanced", state.Data["tag"])
}

func TestEngine_AdvanceNodeToWithMutationPersistsMutation(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := NewEngine(store)

	require.NoError(t, engine.SaveDefinition(ctx, &Definition{
		ID:          "advance-to-with-mutation",
		StartNodeID: "review",
		Nodes: []Node{
			{ID: "review", Edges: NewEdges("approve", "reject")},
			{ID: "approve"},
			{ID: "reject"},
		},
	}))
	require.NoError(t, engine.CreateInstance(ctx, ID("wf-advance-to-mutation"), "advance-to-with-mutation"))
	require.NoError(t, engine.StartInstance(ctx, ID("wf-advance-to-mutation")))

	require.NoError(t, engine.AdvanceNodeToWithMutation(ctx, ID("wf-advance-to-mutation"), "review", "approve", func(st *State, now time.Time) error {
		st.Data["decision"] = "approve"
		return nil
	}))

	state, err := store.Get(ctx, ID("wf-advance-to-mutation"))
	require.NoError(t, err)
	require.Equal(t, "approve", state.Data["decision"])
}

func TestEngine_RejectNodeWithMutationPersistsMutation(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := NewEngine(store)

	require.NoError(t, engine.SaveDefinition(ctx, &Definition{
		ID:          "reject-with-mutation",
		StartNodeID: "start",
		Nodes: []Node{
			{ID: "start", Edges: NewEdges("review")},
			{ID: "review", Edges: NewEdges("approve"), RejectTo: []string{"start"}},
			{ID: "approve"},
		},
	}))
	require.NoError(t, engine.CreateInstance(ctx, ID("wf-reject-mutation"), "reject-with-mutation"))
	require.NoError(t, engine.StartInstance(ctx, ID("wf-reject-mutation")))
	require.NoError(t, engine.AdvanceNode(ctx, ID("wf-reject-mutation"), "start"))

	require.NoError(t, engine.RejectNodeWithMutation(ctx, ID("wf-reject-mutation"), "review", "start", func(st *State, now time.Time) error {
		st.Data["rejected_by"] = "tester"
		return nil
	}))

	state, err := store.Get(ctx, ID("wf-reject-mutation"))
	require.NoError(t, err)
	require.Equal(t, "tester", state.Data["rejected_by"])
}

func TestEngine_LockDefinitionCleansUpReferenceCount(t *testing.T) {
	engine := &Engine{}

	unlock := engine.lockDefinition("def-1")
	engine.definitionMu.Lock()
	require.Contains(t, engine.definitionLocks, "def-1")
	require.Equal(t, 1, engine.definitionLocks["def-1"].refs)
	engine.definitionMu.Unlock()

	unlock()

	engine.definitionMu.Lock()
	require.NotContains(t, engine.definitionLocks, "def-1")
	engine.definitionMu.Unlock()
}

func TestEngine_DefinitionVersioningIsolation(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := NewEngine(store)

	// 发布 v1 定义：start -> step_v1 -> end
	defV1 := &Definition{
		ID:          "multi-ver",
		StartNodeID: "start",
		Nodes: []Node{
			{ID: "start", Edges: NewEdges("step_v1")},
			{ID: "step_v1", Edges: NewEdges("end")},
			{ID: "end"},
		},
	}
	require.NoError(t, engine.SaveDefinition(ctx, defV1))
	require.Equal(t, uint32(1), defV1.Version)

	// 创建实例 1，绑定 v1
	require.NoError(t, engine.CreateInstance(ctx, ID("wf-v1"), "multi-ver"))
	require.NoError(t, engine.StartInstance(ctx, ID("wf-v1")))

	// 发布 v2 定义：直接 start -> step_v2 -> end（移除了 step_v1）
	defV2 := &Definition{
		ID:          "multi-ver",
		StartNodeID: "start",
		Nodes: []Node{
			{ID: "start", Edges: NewEdges("step_v2")},
			{ID: "step_v2", Edges: NewEdges("end")},
			{ID: "end"},
		},
	}
	require.NoError(t, engine.SaveDefinition(ctx, defV2))
	require.Equal(t, uint32(2), defV2.Version)

	// 创建实例 2，默认绑定最新版 v2
	require.NoError(t, engine.CreateInstance(ctx, ID("wf-v2"), "multi-ver"))
	require.NoError(t, engine.StartInstance(ctx, ID("wf-v2")))

	// 实例 1 推进 step_v1：由于多版本隔离，实例 1 仍然绑定 v1 定义，能正常推进至 step_v1
	require.NoError(t, engine.AdvanceNode(ctx, ID("wf-v1"), "start"))
	st1, err := store.Get(ctx, ID("wf-v1"))
	require.NoError(t, err)
	require.Equal(t, uint32(1), st1.DefinitionVersion)
	require.Equal(t, []string{"step_v1"}, st1.ActiveNodeIDs)

	// 实例 2 推进 start：走向 v2 的 step_v2
	require.NoError(t, engine.AdvanceNode(ctx, ID("wf-v2"), "start"))
	st2, err := store.Get(ctx, ID("wf-v2"))
	require.NoError(t, err)
	require.Equal(t, uint32(2), st2.DefinitionVersion)
	require.Equal(t, []string{"step_v2"}, st2.ActiveNodeIDs)
}

func TestEngine_ConditionalRouting(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := NewEngine(store)

	def := &Definition{
		ID:          "cond-route",
		StartNodeID: "check_amount",
		Nodes: []Node{
			{
				ID: "check_amount",
				Edges: []Edge{
					NewConditionalEdge("vip_channel", "data.amount >= 1000"),
					NewDefaultEdge("standard_channel"),
				},
			},
			{ID: "vip_channel"},
			{ID: "standard_channel"},
		},
	}
	require.NoError(t, engine.SaveDefinition(ctx, def))

	// Case 1: amount = 1500 (走向 vip_channel)
	require.NoError(t, engine.CreateInstanceWithData(ctx, ID("wf-cond-1"), "cond-route", map[string]any{"amount": 1500}))
	require.NoError(t, engine.StartInstance(ctx, ID("wf-cond-1")))
	require.NoError(t, engine.AdvanceNode(ctx, ID("wf-cond-1"), "check_amount"))

	st1, err := store.Get(ctx, ID("wf-cond-1"))
	require.NoError(t, err)
	require.Equal(t, []string{"vip_channel"}, st1.ActiveNodeIDs)

	// Case 2: amount = 500 (走向 standard_channel)
	require.NoError(t, engine.CreateInstanceWithData(ctx, ID("wf-cond-2"), "cond-route", map[string]any{"amount": 500}))
	require.NoError(t, engine.StartInstance(ctx, ID("wf-cond-2")))
	require.NoError(t, engine.AdvanceNode(ctx, ID("wf-cond-2"), "check_amount"))

	st2, err := store.Get(ctx, ID("wf-cond-2"))
	require.NoError(t, err)
	require.Equal(t, []string{"standard_channel"}, st2.ActiveNodeIDs)
}

func TestEngine_LifecycleSuspendResumeTerminate(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := NewEngine(store)

	def := &Definition{
		ID:          "lifecycle-def",
		StartNodeID: "step1",
		Nodes: []Node{
			{ID: "step1", Edges: NewEdges("step2")},
			{ID: "step2"},
		},
	}
	require.NoError(t, engine.SaveDefinition(ctx, def))

	// 1. 测试 Suspend 和 Resume
	require.NoError(t, engine.CreateInstance(ctx, ID("wf-life-1"), "lifecycle-def"))
	require.NoError(t, engine.StartInstance(ctx, ID("wf-life-1")))

	// 挂起
	require.NoError(t, engine.SuspendInstance(ctx, ID("wf-life-1"), "waiting for manual audit"))
	st, err := store.Get(ctx, ID("wf-life-1"))
	require.NoError(t, err)
	require.Equal(t, InstanceStatusSuspended, st.Status)

	// 挂起状态下禁止推进
	err = engine.AdvanceNode(ctx, ID("wf-life-1"), "step1")
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Conflict))

	// 恢复
	require.NoError(t, engine.ResumeInstance(ctx, ID("wf-life-1")))
	st, err = store.Get(ctx, ID("wf-life-1"))
	require.NoError(t, err)
	require.Equal(t, InstanceStatusRunning, st.Status)

	// 恢复后可以正常推进
	require.NoError(t, engine.AdvanceNode(ctx, ID("wf-life-1"), "step1"))

	// 2. 测试 Terminate
	require.NoError(t, engine.CreateInstance(ctx, ID("wf-life-2"), "lifecycle-def"))
	require.NoError(t, engine.StartInstance(ctx, ID("wf-life-2")))

	require.NoError(t, engine.TerminateInstance(ctx, ID("wf-life-2"), "cancelled by user"))
	st2, err := store.Get(ctx, ID("wf-life-2"))
	require.NoError(t, err)
	require.Equal(t, InstanceStatusTerminated, st2.Status)
	require.Empty(t, st2.ActiveNodeIDs)

	// 终止状态不可再推进
	err = engine.AdvanceNode(ctx, ID("wf-life-2"), "step1")
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Conflict))
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
