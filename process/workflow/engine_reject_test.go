package workflow

import (
	"context"

	"testing"
	"time"

	"gochen/testkit/require"

	"gochen/errors"
)

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

func TestEngine_updateInstanceData_RejectsNilUpdate(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := NewEngine(store)

	err := engine.updateInstanceData(ctx, ID("wf-5"), nil)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
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
