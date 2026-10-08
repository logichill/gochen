package workflow

import (
	"context"

	"strconv"
	"sync"
	"testing"
	"time"

	"gochen/testkit/require"

	"gochen/errors"
)

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
